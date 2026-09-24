package main

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"time"
)

type accumulator interface {
	add(int64)
	total() int64
}
type counter struct{ value int64 }

func (c *counter) add(delta int64) { c.value += delta }
func (c *counter) total() int64    { return c.value }

type alternateCounter struct{ value int64 }

func (c *alternateCounter) add(delta int64) { c.value += delta }
func (c *alternateCounter) total() int64    { return c.value }
func choose(seed int) accumulator {
	if seed&1 == 0 {
		return &counter{}
	}
	return &alternateCounter{}
}

func main() {
	count, err := strconv.Atoi(os.Args[1])
	if err != nil {
		panic(err)
	}
	allocations, err := strconv.Atoi(os.Args[2])
	if err != nil {
		panic(err)
	}
	if count < 1 || allocations < 1 {
		panic("workload sizes must be positive")
	}
	var before runtime.MemStats
	var after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	start := time.Now()
	var numeric int64
	for i := 0; i < count; i++ {
		numeric += int64(i & 255)
	}
	numericNS := time.Since(start).Nanoseconds()
	runtime.ReadMemStats(&after)
	fmt.Printf("{\"numeric\":{\"ns\":%d,\"sum\":%d,\"allocated_bytes\":%d,\"allocations\":%d},", numericNS, numeric, after.TotalAlloc-before.TotalAlloc, after.Mallocs-before.Mallocs)
	c := choose(count)
	runtime.GC()
	runtime.ReadMemStats(&before)
	start = time.Now()
	for i := 0; i < count; i++ {
		c.add(int64(i & 255))
	}
	dispatchNS := time.Since(start).Nanoseconds()
	runtime.ReadMemStats(&after)
	fmt.Printf("\"dispatch\":{\"ns\":%d,\"sum\":%d,\"allocated_bytes\":%d,\"allocations\":%d},", dispatchNS, c.total(), after.TotalAlloc-before.TotalAlloc, after.Mallocs-before.Mallocs)
	runtime.GC()
	runtime.ReadMemStats(&before)
	start = time.Now()
	items := make([]*counter, 0, allocations)
	for i := 0; i < allocations; i++ {
		item := &counter{}
		item.add(int64(i & 255))
		items = append(items, item)
	}
	var allocatedSum int64
	for _, item := range items {
		allocatedSum += item.total()
	}
	allocationNS := time.Since(start).Nanoseconds()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(items)
	fmt.Printf("\"objects\":{\"ns\":%d,\"sum\":%d,\"allocated_bytes\":%d,\"allocations\":%d,\"heap_bytes_delta\":%d}}\n", allocationNS, allocatedSum, after.TotalAlloc-before.TotalAlloc, after.Mallocs-before.Mallocs, int64(after.HeapAlloc)-int64(before.HeapAlloc))
}
