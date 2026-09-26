package main

import (
	"syscall"
	"unsafe"
)

var counter = syscall.NewLazyDLL("kernel32.dll").NewProc("QueryPerformanceCounter")
var frequency = func() int64 {
	var value int64
	ok, _, err := syscall.NewLazyDLL("kernel32.dll").NewProc("QueryPerformanceFrequency").Call(uintptr(unsafe.Pointer(&value)))
	if ok == 0 || value <= 0 {
		panic(err)
	}
	return value
}()

func tickNow() int64 {
	var value int64
	ok, _, err := counter.Call(uintptr(unsafe.Pointer(&value)))
	if ok == 0 {
		panic(err)
	}
	return value
}
func ticksSeconds(ticks int64) float64 { return float64(ticks) / float64(frequency) }
