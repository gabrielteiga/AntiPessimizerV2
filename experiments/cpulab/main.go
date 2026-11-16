// Command cpulab is an owned, finite workload for automated Windows sampling tests.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"
)

func main() {
	duration := flag.Duration("duration", 15*time.Second, "workload duration")
	threads := flag.Int("threads", 2, "worker threads")
	flag.Parse()
	if *duration <= 0 || *duration > time.Minute || *threads < 1 || *threads > 16 {
		os.Exit(2)
	}
	fmt.Printf("pid=%d\n", os.Getpid())
	end := time.Now().Add(*duration)
	var wg sync.WaitGroup
	for n := 0; n < *threads; n++ {
		wg.Add(1)
		go func(seed uint64) {
			defer wg.Done()
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			for time.Now().Before(end) {
				seed = hotspot(seed)
			}
			runtime.KeepAlive(seed)
		}(uint64(n + 1))
	}
	wg.Wait()
}

//go:noinline
func hotspot(seed uint64) uint64 {
	for i := 0; i < 200000; i++ {
		seed = seed*6364136223846793005 + 1442695040888963407
	}
	return seed
}
