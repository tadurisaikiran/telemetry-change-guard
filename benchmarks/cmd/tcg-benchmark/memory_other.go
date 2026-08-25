//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package main

import "os"

func peakMemoryBytes(_ *os.ProcessState) *uint64 {
	return nil
}
