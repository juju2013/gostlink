// Copyright 2020 Sebastian Lehmann. All rights reserved.
// Use of this source code is governed by a GNU-style
// license that can be found in the LICENSE file.

// this code is mainly inspired and based on the openocd project source code
// for detailed information see

// https://sourceforge.net/p/openocd/code

package gostlink

import (
	"bytes"
	"errors"
	"github.com/google/gousb"
	log "github.com/sirupsen/logrus"
	"time"
)

const AllSupportedVIds = 0xFFFF
const AllSupportedPIds = 0xFFFF

var goStLinkSupportedVIds = []gousb.ID{0x0483} // STLINK Vendor ID
var goStLinkSupportedPIds = []gousb.ID{0x3744, 0x3748, 0x374b, 0x374d, 0x374e, 0x374f, 0x3752, 0x3753}

type stLinkVersion struct {
	/** */
	stlink int
	/** */
	jtag int
	/** */
	swim int
	/** jtag api version supported */
	jtagApi stLinkApiVersion
	/** one bit for each feature supported. See macros STLINK_F_* */
	flags uint32
}

type stLinkTrace struct {
	enabled  bool
	sourceHz uint32
}

/** */
type StLinkHandle struct {
	usbDevice *gousb.Device // reference to libusb device

	usbConfig *gousb.Config // reference to device configuration

	usbInterface *gousb.Interface // reference to currently used interface

	rxEndpoint *gousb.InEndpoint // receive from device endpint

	txEndpoint *gousb.OutEndpoint // transmit to device endpoint

	traceEndpoint *gousb.InEndpoint // endpoint from which trace messages are read from

	transferEndpoint usbTransferEndpoint

	vid gousb.ID // vendor id of device

	pid gousb.ID // product id of device

	stMode StLinkMode

	version stLinkVersion

	trace stLinkTrace

	seggerRtt seggerRttInfo

	reconnectPending bool // reconnect is needed next time we try to query the status

	cmdbuf []byte

	cmdidx uint8

	databuf []byte

	max_mem_packet uint32
}

type StLinkInterfaceConfig struct {
	vid               gousb.ID
	pid               gousb.ID
	mode              StLinkMode
	serial            string
	initialSpeed      uint32
	connectUnderReset bool
}

func NewStLinkConfig(vid gousb.ID, pid gousb.ID, mode StLinkMode,
	serial string, initialSpeed uint32, connectUnderReset bool) *StLinkInterfaceConfig {

	config := &StLinkInterfaceConfig{
		vid:               vid,
		pid:               pid,
		mode:              mode,
		serial:            serial,
		initialSpeed:      initialSpeed,
		connectUnderReset: connectUnderReset,
	}

	return config
}

func NewStLink(config *StLinkInterfaceConfig) (*StLinkHandle, error) {
	var err error
	var devices []*gousb.Device

	handle := &StLinkHandle{}
	handle.stMode = config.mode

	// initialize data buffers for tx and rx
	handle.cmdbuf = make([]byte, cmdBufferSize)
	handle.databuf = make([]byte, dataBufferSize)

	if config.vid == AllSupportedVIds && config.pid == AllSupportedPIds {
		devices, err = usbFindDevices(goStLinkSupportedVIds, goStLinkSupportedPIds)

	} else if config.vid == AllSupportedVIds && config.pid != AllSupportedPIds {
		devices, err = usbFindDevices(goStLinkSupportedVIds, []gousb.ID{config.pid})

	} else if config.vid != AllSupportedVIds && config.pid == AllSupportedPIds {
		devices, err = usbFindDevices([]gousb.ID{config.vid}, goStLinkSupportedPIds)

	} else {
		devices, err = usbFindDevices([]gousb.ID{config.vid}, []gousb.ID{config.pid})
	}

	if len(devices) > 0 {
		if config.serial == "" && len(devices) > 1 {
			return nil, errors.New("could not identity exact stlink by given parameters. (Perhaps a serial no is missing?)")
		} else if len(devices) == 1 {
			handle.usbDevice = devices[0]
		} else {
			for _, dev := range devices {
				devSerialNo, _ := dev.SerialNumber()

				log.Tracef("Compare serial no %s with number %s", devSerialNo, config.serial)

				if devSerialNo == config.serial {
					handle.usbDevice = dev

					log.Infof("Found st link with serial number %s", devSerialNo)
				}
			}
		}
	} else {
		return nil, errors.New("could not find any ST-Link connected to computer")
	}

	if handle.usbDevice == nil {
		return nil, errors.New("critical error during device scan")
	}

	handle.usbDevice.SetAutoDetach(true)

	// no request required configuration an matching usb interface :D
	handle.usbConfig, err = handle.usbDevice.Config(1)
	if err != nil {
		log.Debug(err)
		return nil, errors.New("could not request configuration #1 for st-link debugger")
	}

	handle.usbInterface, err = handle.usbConfig.Interface(0, 0)
	if err != nil {
		log.Debug(err)
		return nil, errors.New("could not claim interface 0,0 for st-link debugger")
	}

	// now determine different endpoints
	// RX-Endpoint is the same for alle devices

	handle.rxEndpoint, err = handle.usbInterface.InEndpoint(usbRxEndpointNo)

	if err != nil {
		return nil, errors.New("could get rx endpoint for debugger")
	}

	var errorTx, errorTrace error

	switch handle.usbDevice.Desc.Product {
	case stLinkV1Pid:
		handle.version.stlink = 1
		handle.txEndpoint, errorTx = handle.usbInterface.OutEndpoint(usbTxEndpointNo)

	case stLinkV3UsbLoaderPid, stLinkV3EPid, stLinkV3SPid, stLinkV32VcpPid:
		handle.version.stlink = 3
		handle.txEndpoint, errorTx = handle.usbInterface.OutEndpoint(usbTxEndpointApi2v1)
		handle.traceEndpoint, errorTrace = handle.usbInterface.InEndpoint(usbTraceEndpointApi2v1)

	case stLinkV21Pid, stLinkV21NoMsdPid:
		handle.version.stlink = 2
		handle.txEndpoint, errorTx = handle.usbInterface.OutEndpoint(usbTxEndpointApi2v1)
		handle.traceEndpoint, errorTrace = handle.usbInterface.InEndpoint(usbTraceEndpointApi2v1)

	default:
		log.Infof("Could not determine pid of debugger %04x. Assuming Link V2", handle.usbDevice.Desc.Product)
		handle.version.stlink = 2

		handle.txEndpoint, errorTx = handle.usbInterface.OutEndpoint(usbTxEndpointNo)
		handle.traceEndpoint, errorTrace = handle.usbInterface.InEndpoint(usbTraceEndpointNo)
	}

	if errorTrace != nil {
		return nil, errors.New("could not get trace endpoint of debugger")
	}

	if errorTx != nil {
		return nil, errors.New("could not get tx endpoint of device")
	}

	err = handle.usbGetVersion()

	if err != nil {
		return nil, err
	}

	switch handle.stMode {
	case StLinkModeDebugSwd:
		if handle.version.jtagApi == jTagApiV1 {
			return nil, errors.New("swd not supported by jtag api v1")
		}
	case StLinkModeDebugJtag:
		if handle.version.jtag == 0 {
			return nil, errors.New("jtag transport not supported by stlink")
		}
	case StLinkModeDebugSwim:
		if handle.version.swim == 0 {
			return nil, errors.New("swim transport not supported by device")
		}

	default:
		return nil, errors.New("unknown ST-Link mode")
	}

	err = handle.usbInitMode(config.connectUnderReset, config.initialSpeed)

	if err != nil {
		return nil, err
	}

	/**
		TODO: Implement SWIM mode configuration
	if (h->st_mode == STLINK_MODE_DEBUG_SWIM) {
		err = stlink_swim_enter(h);
		if (err != ERROR_OK) {
			LOG_ERROR("stlink_swim_enter_failed (unable to connect to the target)");
			goto error_open;
		}
		*fd = h;
		h->max_mem_packet = STLINK_DATA_SIZE;
		return ERROR_OK;
	}
	*/

	handle.max_mem_packet = 1 << 10

	err = handle.usbInitAccessPort(0)

	if err != nil {
		return nil, err
	}

	buffer := bytes.NewBuffer([]byte{})
	errCode := handle.usbReadMem32(cpuIdBaseRegister, 4, buffer)

	if errCode == nil {
		var cpuid uint32 = le_to_h_u32(buffer.Bytes())
		var i uint32 = (cpuid >> 4) & 0xf

		log.Debugf("Got CpuID: %08x", cpuid)

		if i == 4 || i == 3 {
			/* Cortex-M3/M4 has 4096 bytes autoincrement range */
			log.Debug("Set mem packet layout according to Cortex M3/M4")
			handle.max_mem_packet = 1 << 12
		}
	}

	log.Debugf("Using TAR autoincrement: %d", handle.max_mem_packet)
	return handle, nil
}

func (h *StLinkHandle) Close() {
	if h.usbDevice != nil {
		log.Debugf("Close ST-Link device [%04x:%04x]", uint16(h.vid), uint16(h.pid))

		h.usbInterface.Close()
		h.usbConfig.Close()
		h.usbDevice.Close()
	} else {
		log.Warn("Tried to close invalid stlink handle")
	}
}

func (h *StLinkHandle) GetTargetVoltage() (float32, error) {
	var adcResults [2]uint32

	/* no error message, simply quit with error */
	if (h.version.flags & flagHasTargetVolt) == 0 {
		return -1.0, errors.New("device does not support voltage measurement")
	}

	h.usbInitBuffer(transferRxEndpoint, 8)

	h.cmdbuf[h.cmdidx] = cmdGetTargetVoltage
	h.cmdidx++

	err := h.usbTransferNoErrCheck(h.databuf, 8)

	if err != nil {
		return -1.0, err
	}

	/* convert result */
	adcResults[0] = le_to_h_u32(h.databuf)
	adcResults[1] = le_to_h_u32(h.databuf[4:])

	var targetVoltage float32 = 0.0

	if adcResults[0] > 0 {
		targetVoltage = 2 * (float32(adcResults[1]) * (1.2 / float32(adcResults[0])))
	}

	log.Infof("Target voltage: %f", targetVoltage)

	return targetVoltage, nil
}

func (h *StLinkHandle) GetIdCode() (uint32, error) {
	var offset int
	var retVal error

	if h.stMode == StLinkModeDebugSwim {
		return 0, nil
	}

	h.usbInitBuffer(transferRxEndpoint, 12)

	h.cmdbuf[h.cmdidx] = cmdDebug
	h.cmdidx++

	if h.version.jtagApi == jTagApiV1 {
		h.cmdbuf[h.cmdidx] = debugReadCoreId
		h.cmdidx++

		retVal = h.usbTransferNoErrCheck(h.databuf, 4)
		offset = 0
	} else {
		h.cmdbuf[h.cmdidx] = debugApiV2ReadIdCodes
		h.cmdidx++

		retVal = h.usbTransferErrCheck(h.databuf, 12)
		offset = 4
	}

	if retVal != nil {
		return 0, retVal

	} else {
		idCode := le_to_h_u32(h.databuf[offset:])

		return idCode, nil
	}
}
func (h *StLinkHandle) SetSpeed(khz uint32, query bool) (uint32, error) {

	switch h.stMode {
	/*case STLINK_MODE_DEBUG_SWIM:
	return stlink_speed_swim(khz, query)
	*/

	case StLinkModeDebugSwd:
		if h.version.jtagApi == jTagApiV3 {
			return h.setSpeedV3(false, khz, query)
		} else {
			return h.setSpeedSwd(khz, query)
		}

	/*case STLINK_MODE_DEBUG_JTAG:
	if h.version.jtag_api == STLINK_JTAG_API_V3 {
		return stlink_speed_v3(true, khz, query)
	} else {
		return stlink_speed_jtag(khz, query)
	}
	*/
	default:
		return khz, errors.New("requested ST-Link mode not supported yet")
	}
}

func (h *StLinkHandle) ConfigTrace(enabled bool, tpiuProtocol TpuiPinProtocolType, portSize uint32,
	traceFreq *uint32, traceClkInFreq uint32, preScaler *uint16) error {

	if enabled == true && ((h.version.flags&flagHasTrace == 0) || tpiuProtocol != TpuiPinProtocolAsyncUart) {
		return errors.New("the attached ST-Link version does not support this trace mode")
	}

	if !enabled {
		h.usbTraceDisable()
		return nil
	}

	if *traceFreq > traceMaxHz {
		return errors.New("this ST-Link version does not support frequency")
	}

	h.usbTraceDisable()

	if *traceFreq == 0 {
		*traceFreq = traceMaxHz
	}

	presc := uint16(traceClkInFreq / *traceFreq)

	if (traceClkInFreq % *traceFreq) > 0 {
		presc++
	}

	if presc > tpuiAcprMaxSwoScaler {
		return errors.New("SWO frequency is not suitable. Please choose a different")
	}

	*preScaler = presc
	h.trace.sourceHz = *traceFreq

	return h.usbTraceEnable()
}

func (h *StLinkHandle) ReadMem(addr uint32, bitLength MemoryBlockSize, count uint32, buffer *bytes.Buffer) error {
	var retErr error
	var bytesRemaining uint32 = 0
	var retries int = 0
	var bufferPos uint32 = 0

	/* calculate byte count */
	count *= uint32(bitLength)

	/* switch to 8 bit if stlink does not support 16 bit memory read */
	if bitLength == Memory16BitBlock && ((h.version.flags & flagHasMem16Bit) == 0) {
		bitLength = Memory8BitBlock
		log.Debug("ST-Link does not support 16bit transfer")
	}

	for count > 0 {

		if bitLength != Memory8BitBlock {
			bytesRemaining = h.maxBlockSize(h.max_mem_packet, addr)
		} else {
			bytesRemaining = h.usbBlock()
		}

		if count < bytesRemaining {
			bytesRemaining = count
		}

		/*
		* all stlink support 8/32bit memory read/writes and only from
		* stlink V2J26 there is support for 16 bit memory read/write.
		* Honour 32 bit and, if psizeossible, 16 bit too. Otherwise, handle
		* as 8bit access.
		 */
		if bitLength != Memory8BitBlock {
			/* When in jtag mode the stlink uses the auto-increment functionality.
			 	* However it expects us to pass the data correctly, this includes
			 	* alignment and any page boundaries. We already do this as part of the
			 	* adi_v5 implementation, but the stlink is a hla adapter and so this
			 	* needs implementing manually.
				 * currently this only affects jtag mode, according to ST they do single
				 * access in SWD mode - but this may change and so we do it for both modes */

			// we first need to check for any unaligned bytes
			if (addr & (uint32(bitLength) - 1)) > 0 {
				var headBytes = uint32(bitLength) - (addr & (uint32(bitLength) - 1))

				log.Debug("Read unaligned bytes")

				err := h.usbReadMem8(addr, uint16(headBytes), buffer)

				if err != nil {
					usbError := err.(*usbError)

					if usbError.UsbErrorCode == usbErrorWait && retries < maximumWaitRetries {
						var sleepDur time.Duration = 1 << retries
						retries++

						time.Sleep(sleepDur * 1000000)
						continue
					}

					return err
				}

				bufferPos += headBytes
				addr += headBytes
				count -= headBytes
				bytesRemaining -= headBytes

				log.Debugf("BufPos: %d, Addr: %08x, Count: %d, BytesRemain: %d", bufferPos, addr, count, bytesRemaining)
			}

			if (bytesRemaining & (uint32(bitLength) - 1)) > 0 {
				retErr = h.ReadMem(addr, 1, bytesRemaining, buffer)
			} else if bitLength == Memory16BitBlock {
				retErr = h.usbReadMem16(addr, uint16(bytesRemaining), buffer)
			} else {
				retErr = h.usbReadMem32(addr, uint16(bytesRemaining), buffer)
			}
		} else {
			retErr = h.usbReadMem8(addr, uint16(bytesRemaining), buffer)
		}

		if retErr != nil {
			usbError := retErr.(*usbError)

			if usbError.UsbErrorCode == usbErrorWait && retries < maximumWaitRetries {
				var sleepDur time.Duration = 1 << retries
				retries++

				time.Sleep(sleepDur * 1000000)
				continue
			}

			return retErr
		}

		bufferPos += bytesRemaining
		addr += bytesRemaining
		count -= bytesRemaining
	}

	return retErr
}

func (h *StLinkHandle) WriteMem(address uint32, bitLength MemoryBlockSize, count uint32, buffer []byte) error {
	var retError error
	var bytesRemaining uint32
	retries := 0
	var bufferPos uint32 = 0

	count *= uint32(bitLength)

	if bitLength == Memory16BitBlock && (h.version.flags&flagHasMem16Bit) == 0 {
		log.Debug("Set 16bit memory read to 8bit")
		bitLength = Memory8BitBlock
	}

	for count > 0 {
		if bitLength != Memory8BitBlock {
			bytesRemaining = h.maxBlockSize(h.max_mem_packet, address)
		} else {
			bytesRemaining = h.usbBlock()
		}

		if count < bytesRemaining {
			bytesRemaining = count
		}

		//	all stlink support 8/32bit memory read/writes and only from
		//	stlink V2J26 there is support for 16 bit memory read/write.
		//  Honour 32 bit and, if possible, 16 bit too. Otherwise, handle
		//  as 8bit access.

		if bitLength != Memory8BitBlock {

			/* When in jtag mode the stlink uses the auto-increment functionality.
			 * However it expects us to pass the data correctly, this includes
			 * alignment and any page boundaries. We already do this as part of the
			 * adi_v5 implementation, but the stlink is a hla adapter and so this
			 * needs implementing manually.
			 * currently this only affects jtag mode, according to ST they do single
			 * access in SWD mode - but this may change and so we do it for both modes
			 */

			// we first need to check for any unaligned bytes
			if (address & (uint32(bitLength) - 1)) > 0 {
				var headBytes = uint32(bitLength) - (address & (uint32(bitLength) - 1))

				err := h.usbWriteMem8(address, uint16(headBytes), buffer)

				if err != nil {
					usbError := err.(*usbError)

					if usbError.UsbErrorCode == usbErrorWait && retries < maximumWaitRetries {
						var sleepDur time.Duration = 1 << retries
						retries++

						time.Sleep(sleepDur * 1000000)
						continue
					}

					return err
				}

				bufferPos += headBytes
				address += headBytes
				count -= headBytes
				bytesRemaining -= headBytes

				log.Debugf("BufPos: %d, Addr: %08x, Count: %d, BytesRemain: %d", bufferPos, address, count, bytesRemaining)
			}

			if (bytesRemaining & (uint32(bitLength) - 1)) > 0 {
				retError = h.WriteMem(address, 1, bytesRemaining, buffer[bufferPos:])
			} else if bitLength == Memory16BitBlock {
				retError = h.usbWriteMem16(address, uint16(bytesRemaining), buffer[bufferPos:])
			} else {
				retError = h.usbWriteMem32(address, uint16(bytesRemaining), buffer[bufferPos:])
			}
		} else {
			retError = h.usbWriteMem8(address, uint16(bytesRemaining), buffer)
		}

		if retError != nil {
			switch retError.(type) {
			case gousb.TransferStatus:
				log.Debug("Got usb transfer error state ", retError)
				var sleepDur time.Duration = 1 << retries
				retries++

				time.Sleep(sleepDur * 1000000)
				continue

			case *usbError:
				usbError := retError.(*usbError)

				if usbError.UsbErrorCode == usbErrorWait && retries < maximumWaitRetries {
					var sleepDur time.Duration = 1 << retries
					retries++

					time.Sleep(sleepDur * 1000000)
					continue
				}
			}

			return retError
		}

		bufferPos += bytesRemaining
		address += bytesRemaining
		count -= bytesRemaining
	}

	return retError
}

func (h *StLinkHandle) PollTrace(buffer []byte, size *uint32) error {

	if h.trace.enabled == true && (h.version.flags&flagHasTrace) != 0 {
		h.usbInitBuffer(transferRxEndpoint, 10)

		h.cmdbuf[h.cmdidx] = cmdDebug
		h.cmdidx++

		h.cmdbuf[h.cmdidx] = debugApiV2GetTraceNB
		h.cmdidx++

		err := h.usbTransferNoErrCheck(h.databuf, 2)

		if err != nil {
			return err
		}

		bytesAvailable := uint32(le_to_h_u16(h.databuf))

		if bytesAvailable < *size {
			*size = bytesAvailable
		} else {
			*size = *size - 1
		}

		if *size > 0 {
			return h.usbReadTrace(buffer, *size)
		}
	}

	*size = 0
	return nil
}

func (h *StLinkHandle) Reset() {
	h.usbDevice.Reset()
}