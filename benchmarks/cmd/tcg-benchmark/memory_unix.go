//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package main

import (
	"os"
	"runtime"
	"syscall"
)

func peakMemoryBytes(state *os.ProcessState) *uint64 {
	if state == nil {
		return nil
	}
	usage, ok := state.SysUsage().(*syscall.Rusage)
	if !ok || usage.Maxrss <= 0 {
		return nil
	}
	value := uint64(usage.Maxrss)
	if runtime.GOOS != "darwin" {
		value *= 1024
	}
	return &value
}
