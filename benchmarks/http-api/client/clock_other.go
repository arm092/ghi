//go:build !windows

package main

import "time"

var clockOrigin = time.Now()

func tickNow() int64                   { return time.Since(clockOrigin).Nanoseconds() }
func ticksSeconds(ticks int64) float64 { return float64(ticks) / 1e9 }
