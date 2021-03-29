// Copyright 2021 juju2013@github. All rights reserved.
// Use of this source code is governed by a GNU-style
// license that can be found in the LICENSE file.

package gostlink

import (
  "encoding/binary"
)

type TargetRegisters struct {
  Status      uint32
  R           [16]uint32
  XPSR        uint32
  MainSP      uint32
  ProcessSP   uint32
  RW          uint32
  RW2         uint32
}

// Get all registers content
func (h *StLink) GetRegisters() (*TargetRegisters, error) {
  if err:=h.UsbModeEnter(StLinkModeDebugSwd); err !=nil {
    return nil, err
  }
  defer h.UsbLeaveMode(StLinkModeDebugSwd)
  
  ctx := h.initTransfer(transferIncoming)
  ctx.cmdBuf.WriteByte(cmdDebug)
  ctx.cmdBuf.WriteByte(debugApiV2ReadAllRegs)

  regs := TargetRegisters{}
  err := h.usbTransferNoErrCheck(ctx, uint32(binary.Size(regs)))
  if err != nil {
    return nil, err
  }

  regs.Status = ctx.dataBuf.ReadUint32LE()
  for i := range regs.R {
    regs.R[i] = ctx.dataBuf.ReadUint32LE()
  }
  regs.XPSR = ctx.dataBuf.ReadUint32LE()
  regs.MainSP = ctx.dataBuf.ReadUint32LE()
  regs.ProcessSP = ctx.dataBuf.ReadUint32LE()
  regs.RW = ctx.dataBuf.ReadUint32LE()
  regs.RW2 = ctx.dataBuf.ReadUint32LE()
  return &regs, nil
}

// Get one register content
func (h *StLink) GetRegister(register uint8) (uint32, error) {
  if err:=h.UsbModeEnter(StLinkModeDebugSwd); err !=nil {
    return 0, err
  }
  defer h.UsbLeaveMode(StLinkModeDebugSwd)
  
  ctx := h.initTransfer(transferIncoming)
  ctx.cmdBuf.WriteByte(cmdDebug)
  ctx.cmdBuf.WriteByte(debugApiV2ReadReg)
  ctx.cmdBuf.WriteByte(register)

  err := h.usbTransferNoErrCheck(ctx, 8)
  if err != nil {
    return 0, err
  }
  ctx.dataBuf.ReadUint32LE() // Status
  return ctx.dataBuf.ReadUint32LE(), nil
}
