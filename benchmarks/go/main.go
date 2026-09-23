package main

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type counter struct{ value int64 }

func (c *counter) add(delta int64) { c.value += delta }
func (c *counter) total() int64    { return c.value }

func main() {
	count, err := strconv.Atoi(os.Args[1])
	if err != nil {
		panic(err)
	}
	start := time.Now()
	var numeric int64
	for i := 0; i < count; i++ {
		numeric += int64(i & 255)
	}
	numericNS := time.Since(start).Nanoseconds()
	c := &counter{}
	start = time.Now()
	for i := 0; i < count; i++ {
		c.add(int64(i & 255))
	}
	objectNS := time.Since(start).Nanoseconds()
	fmt.Printf("%d %d %d %d\n", numericNS, objectNS, numeric, c.total())
}
