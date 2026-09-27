package main

import (
	"encoding/json"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
)

func init() {
	address := os.Getenv("JOURNAL_METRICS_ADDR")
	if address == "" {
		return
	}
	http.HandleFunc("/bench/memory", func(w http.ResponseWriter, r *http.Request) {
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		_ = json.NewEncoder(w).Encode(map[string]uint64{"allocations": stats.Mallocs, "bytes": stats.TotalAlloc, "heap": stats.HeapAlloc})
	})
	go func() { panic(http.ListenAndServe(address, http.DefaultServeMux)) }()
}
