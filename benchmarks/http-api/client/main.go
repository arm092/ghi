// Closed-loop HTTP load client. Every response must match the reference bytes.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	url := flag.String("url", "", "endpoint")
	expected := flag.String("body", "", "expected response file")
	status := flag.Int("status", 200, "expected status")
	workers := flag.Int("workers", 1, "concurrent clients")
	requests := flag.Int("requests", 5000, "requests")
	flag.Parse()
	body, err := os.ReadFile(*expected)
	if err != nil {
		panic(err)
	}
	transport := &http.Transport{MaxIdleConns: *workers, MaxIdleConnsPerHost: *workers, MaxConnsPerHost: *workers}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 20 * time.Second}
	var next, failed atomic.Int64
	latencies := make([]int64, *requests)
	startGate := make(chan struct{})
	var ready, done sync.WaitGroup
	for w := 0; w < *workers; w++ {
		ready.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			ready.Done()
			<-startGate
			for {
				index := int(next.Add(1)) - 1
				if index >= *requests {
					return
				}
				started := tickNow()
				response, err := client.Get(*url)
				if err != nil {
					failed.Add(1)
					continue
				}
				data, readErr := io.ReadAll(response.Body)
				response.Body.Close()
				latencies[index] = int64(ticksSeconds(tickNow()-started) * 1e9)
				if readErr != nil || response.StatusCode != *status || !bytes.Equal(data, body) {
					failed.Add(1)
				}
			}
		}()
	}
	ready.Wait()
	started := tickNow()
	close(startGate)
	done.Wait()
	elapsed := ticksSeconds(tickNow() - started)
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	result := map[string]any{"requests": *requests, "workers": *workers, "errors": failed.Load(), "seconds": elapsed, "requests_per_second": float64(*requests) / elapsed, "p50_ms": float64(latencies[len(latencies)*50/100]) / 1e6, "p95_ms": float64(latencies[len(latencies)*95/100]) / 1e6, "p99_ms": float64(latencies[len(latencies)*99/100]) / 1e6}
	encoded, _ := json.Marshal(result)
	fmt.Println(string(encoded))
	if failed.Load() != 0 {
		os.Exit(1)
	}
}
