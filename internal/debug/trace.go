// Copyright 2016 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package debug

import (
	"context"
	"errors"
	"os"
	"runtime/trace"
	"strconv"

	"github.com/ethereum/go-ethereum/log"
)

// StartGoTrace turns on tracing, writing to the given file.
// only do file open
func (h *HandlerT) StartGoTrace(file string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.traceW != nil {
		return errors.New("trace file already opened")
	}

	var fileName string
	if h.traceFile == "" {
		h.traceFile = file
		fileName = file
	} else {
		fileName = h.traceFile + "_" + strconv.Itoa(int(h.curBlockNum)) + "_" + h.fileSubfix
	}
	f, err := os.Create(expandHome(fileName))
	if err != nil {
		log.Info("StartGoTrace file created", "file", fileName, "err", err)
		return err
	}

	h.traceW = f

	log.Info("StartGoTrace file created", "file", fileName)
	/*
		if err := trace.Start(f); err != nil {
			f.Close()
			return err
		}
		h.ctx, h.task = trace.NewTask(context.Background(), "larryDebugTask")
	*/
	return nil
}

// user controled start & stop capture
func (h *HandlerT) RpcEnableTraceCapture() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// already running
	if h.task != nil {
		log.Info("trace task is already running")
		return nil
	}

	// create file
	h.mu.Unlock()
	h.StartGoTrace("")
	h.mu.Lock()
	f := h.traceW
	if err := trace.Start(f); err != nil {
		f.Close()
		log.Error("EnableTrace Start failed", "err", err)
		h.traceW = nil
		return err
	}

	h.ctx, h.task = trace.NewTask(context.Background(), "larryDebugTask")
	log.Info("Go tracing started")
	return nil
}

func (h *HandlerT) RpcDisableTraceCapture() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.traceW == nil {
		return errors.New("trace not in progress")
	}

	if h.task == nil {
		log.Error("StopGoTrace task is nil!")
	} else {
		h.task.End()
	}
	h.task = nil

	trace.Stop()
	log.Info("Done writing Go trace")
	h.traceW.Close()
	h.traceW = nil
	return nil
}

func (h *HandlerT) RpcEnableTraceCaptureWithBlockRange(number, length uint64) {
	h.startBlockNum = number
	h.endBlockNum = number + length

	log.Info("enable traceCapture", "startBlockNum", h.startBlockNum,
		"endBlockNum", h.endBlockNum)
}

func (h *HandlerT) RpcEnableTraceCaptureBigBlock(number, threshold, length uint64) {
	h.startBlockNum = number
	h.traceBigBlock = true
	h.bigBlockThreshold = threshold
	h.traceBigNum = length
	log.Info("enable big block traceCapture", "startBlockNum", h.startBlockNum,
		"threshold", threshold, "length", length)
}

// enable a trace capture cycle, with length captureBlockNum
func (h *HandlerT) EnableTraceCapture(blockNum uint64, subfix string) bool {
	h.fileSubfix = subfix
	if blockNum >= h.startBlockNum && blockNum < h.endBlockNum {
		h.curBlockNum = blockNum
		h.RpcEnableTraceCapture()
		return true
	}

	h.RpcDisableTraceCapture()
	return false
}

func (h *HandlerT) EnableTraceBigBlock(blockNum uint64, txNum int, subfix string) bool {
	h.fileSubfix = subfix
	h.RpcDisableTraceCapture()
	if blockNum >= h.startBlockNum && h.traceBigBlock && txNum >= int(h.bigBlockThreshold) {
		h.curBlockNum = blockNum
		h.traceBigNum--
		if h.traceBigNum == 0 {
			h.traceBigBlock = false
		}
		h.RpcEnableTraceCapture()
		return true
	}
	return false
}
func (h *HandlerT) Ctx() context.Context {
	return h.ctx
}

func (h *HandlerT) Task() *trace.Task {
	return h.task
}

// StopTrace stops an ongoing trace.
func (h *HandlerT) StopGoTrace() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.traceW == nil {
		return errors.New("trace not in progress")
	}

	if h.task == nil {
		log.Error("StopGoTrace task is nil!")
	} else {
		h.task.End()
	}
	h.task = nil

	trace.Stop()
	log.Info("Done writing Go trace", "dump", h.traceFile)
	h.traceW.Close()
	h.traceW = nil
	return nil
}

func (h *HandlerT) LogWhenTracing(msg string) {
	if h.task == nil {
		return
	}
	log.Debug("LogWhenTracing", "msg", msg)
}

func (h *HandlerT) StartRegionAuto(msg string) func() {
	// log.Info("HandlerT StartRegion enter", "msg", msg)
	if h.task == nil {
		return func() {
			// log.Info("HandlerT StartRegion exit not started")
		}
	}

	// task ready, do trace
	// log.Info("StartRegionAuto enter", "msg", msg)
	region := trace.StartRegion(h.ctx, msg)
	return func() {
		// log.Info("StartRegionAuto exit", "msg", msg)
		region.End()
	}
}

func (h *HandlerT) StartTrace(msg string) *trace.Region {
	if h.task == nil {
		return nil
	}
	return trace.StartRegion(h.ctx, msg)
}

func (h *HandlerT) EndTrace(region *trace.Region) {
	if h.task == nil || region == nil {
		return
	}
	region.End()
}
