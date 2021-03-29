// Copyright 2020 Sebastian Lehmann. All rights reserved.
// Use of this source code is governed by a GNU-style
// license that can be found in the LICENSE file.

package gostlink

import (
	"bytes"
	"fmt"
)

// Read (len * 1) bytes from Target's memory
func (h *StLink) UsbReadMem8(addr uint32, len uint16, buffer *bytes.Buffer) error {
	var readLen = uint32(len)

	/* max 8 bit read/write is 64 bytes or 512 bytes for v3 */
	if readLen > h.usbBlock() {
		return newUsbError(fmt.Sprintf("max buffer (%d) length exceeded", h.usbBlock()), usbErrorFail)
	}

	ctx := h.initTransfer(transferIncoming)

	ctx.cmdBuf.WriteByte(cmdDebug)
	ctx.cmdBuf.WriteByte(debugReadMem8Bit)

	ctx.cmdBuf.WriteUint32LE(addr)
	ctx.cmdBuf.WriteUint16LE(len)

	// we need to fix read length for single bytes
	if readLen == 1 {
		readLen++
	}

	err := h.usbTransferNoErrCheck(ctx, readLen)

	if err != nil {
		return newUsbError(fmt.Sprintf("ReadMem8 transfer error occurred"), usbErrorFail)

	}

	buffer.Write(ctx.DataBytes())

	return h.usbGetReadWriteStatus()
}

// Read ((len/2) * 2) bytes from Target's memory, addr must be 16bit aligned
func (h *StLink) UsbReadMem16(addr uint32, len uint16, buffer *bytes.Buffer) error {
	if !h.version.flags.Get(flagHasMem16Bit) {
		return newUsbError("Read16 command not supported by device", usbErrorCommandNotFound)
	}

	/* data must be a multiple of 2 and half-word aligned */
	if ((len % 2) > 0) || ((addr % 2) > 0) {
		return newUsbError("ReadMem16 Invalid data alignment", usbErrorTargetUnalignedAccess)
	}

	ctx := h.initTransfer(transferIncoming)

	ctx.cmdBuf.WriteByte(cmdDebug)
	ctx.cmdBuf.WriteByte(debugApiV2ReadMem16Bit)

	ctx.cmdBuf.WriteUint32LE(addr)
	ctx.cmdBuf.WriteUint16LE(len)

	err := h.usbTransferNoErrCheck(ctx, uint32(len))

	if err != nil {
		return newUsbError("ReadMem16 transfer error occurred", usbErrorFail)
	}

	buffer.Write(ctx.DataBytes())

	return h.usbGetReadWriteStatus()
}

// Read ((len/4) * 4) bytes from Target's memory, addr must be 32bit aligned
func (h *StLink) UsbReadMem32(addr uint32, len uint16, buffer *bytes.Buffer) error {

	/* data must be a multiple of 4 and word aligned */
	if ((len % 4) > 0) || ((addr % 4) > 0) {
		return newUsbError("ReadMem32 Invalid data alignment", usbErrorTargetUnalignedAccess)
	}

	ctx := h.initTransfer(transferIncoming)

	ctx.cmdBuf.WriteByte(cmdDebug)
	ctx.cmdBuf.WriteByte(debugReadMem32Bit)

	ctx.cmdBuf.WriteUint32LE(addr)
	ctx.cmdBuf.WriteUint16LE(len)

	err := h.usbTransferNoErrCheck(ctx, uint32(len))

	if err != nil {
		return newUsbError("ReadMem32 transfer error occurred", usbErrorFail)
	}

	buffer.Write(ctx.DataBytes())

	return h.usbGetReadWriteStatus()
}

// Read len bytes from Target's memory, NO aligment needed for add and len 
func (h *StLink) UsbReadMem(addr uint32, len uint16, buffer *bytes.Buffer) error {

  // Read 8 bits until we get a 32bit aligned addr
  prelen := uint16(addr % 4)
  if (prelen > 0) {
    prelen = 4 - prelen
    if err := h.UsbReadMem8(addr, prelen, buffer); err != nil {
      return err
    }
  }
  
  // Read as many 32bit as needed
  w32len := uint16((len - prelen) / 4)*4
  if (w32len > 0) {
    if err := h.UsbReadMem32((addr+uint32(prelen)), w32len, buffer); err !=nil {
        return err
    } 
  }
  
  // Read remaining bytes by 8bit's Read
  postlen := len - w32len - prelen
  if (postlen > 0) {
    if err := h.UsbReadMem8(addr+uint32(prelen+w32len), postlen, buffer); err != nil {
      return err
    }
  }
  return nil
}

func (h *StLink) UsbWriteMem8(addr uint32, len uint16, buffer []byte) error {
	writeLen := uint32(len)

	if writeLen > h.usbBlock() {
		return newUsbError(fmt.Sprintf("max buffer (%d) length exceeded", h.usbBlock()), usbErrorFail)
	}

	ctx := h.initTransfer(transferOutgoing)

	ctx.cmdBuf.WriteByte(cmdDebug)
	ctx.cmdBuf.WriteByte(debugWriteMem8Bit)

	ctx.cmdBuf.WriteUint32LE(addr)
	ctx.cmdBuf.WriteUint16LE(len)

	ctx.dataBuf.Write(buffer[:len])

	err := h.usbTransferNoErrCheck(ctx, writeLen)

	if err != nil {
		return err
	}

	return h.usbGetReadWriteStatus()
}

func (h *StLink) UsbWriteMem16(addr uint32, len uint16, buffer []byte) error {
	writeLen := uint32(len)

	if !h.version.flags.Get(flagHasMem16Bit) {
		return newUsbError("Read16 command not supported by device", usbErrorCommandNotFound)
	}

	/* data must be a multiple of 2 and half-word aligned */
	if ((len % 2) > 0) || ((addr % 2) > 0) {
		return newUsbError("ReadMem16 Invalid data alignment", usbErrorTargetUnalignedAccess)
	}

	ctx := h.initTransfer(transferOutgoing)

	ctx.cmdBuf.WriteByte(cmdDebug)
	ctx.cmdBuf.WriteByte(debugApiV2WriteMem16Bit)

	ctx.cmdBuf.WriteUint32LE(addr)
	ctx.cmdBuf.WriteUint16LE(len)

	ctx.dataBuf.Write(buffer[:len])

	err := h.usbTransferNoErrCheck(ctx, writeLen)

	if err != nil {
		return err
	}

	return h.usbGetReadWriteStatus()
}

func (h *StLink) UsbWriteMem32(addr uint32, len uint16, buffer []byte) error {
	writeLen := uint32(len)

	/* data must be a multiple of 4 and word aligned */
	if ((len % 4) > 0) || ((addr % 4) > 0) {
		return newUsbError("ReadMem32 Invalid data alignment", usbErrorTargetUnalignedAccess)
	}

	ctx := h.initTransfer(transferOutgoing)

	ctx.cmdBuf.WriteByte(cmdDebug)
	ctx.cmdBuf.WriteByte(debugWriteMem32Bit)

	ctx.cmdBuf.WriteUint32LE(addr)
	ctx.cmdBuf.WriteUint16LE(len)

	ctx.dataBuf.Write(buffer[:len])

	err := h.usbTransferNoErrCheck(ctx, writeLen)

	if err != nil {
		return err
	}

	return h.usbGetReadWriteStatus()
}
