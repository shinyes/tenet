package main

import (
	"flag"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shinyes/tenet/node"
)

func main() {
	var (
		total       = flag.Int("total", 1200, "total dial attempts")
		concurrency = flag.Int("concurrency", 200, "concurrent dial workers")
		hold        = flag.Duration("hold", 1200*time.Millisecond, "how long to hold each connection")
		dialTimeout = flag.Duration("dial-timeout", 1500*time.Millisecond, "per-connection dial timeout")
	)
	flag.Parse()

	n, err := node.NewNode(
		node.WithNetworkPassword("stress-local"),
		node.WithListenPort(0),
		node.WithEnableRelay(false),
		node.WithEnableHolePunch(false),
		node.WithEnableReconnect(false),
	)
	if err != nil {
		panic(fmt.Errorf("new node failed: %w", err))
	}
	if err := n.Start(); err != nil {
		panic(fmt.Errorf("start node failed: %w", err))
	}
	defer n.Stop()

	target := fmt.Sprintf("127.0.0.1:%d", n.LocalAddr.Port)
	fmt.Printf("stress target: %s\n", target)
	fmt.Printf("total=%d concurrency=%d hold=%s dial-timeout=%s\n", *total, *concurrency, hold.String(), dialTimeout.String())

	var (
		start      = time.Now()
		success    atomic.Int64
		dialFailed atomic.Int64
		closedFast atomic.Int64
	)

	jobs := make(chan struct{}, *total)
	var wg sync.WaitGroup

	workerN := *concurrency
	if workerN < 1 {
		workerN = 1
	}
	if workerN > *total {
		workerN = *total
	}

	for i := 0; i < workerN; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d := net.Dialer{Timeout: *dialTimeout}
			probe := []byte{0xAB}
			for range jobs {
				conn, err := d.Dial("tcp", target)
				if err != nil {
					dialFailed.Add(1)
					continue
				}

				success.Add(1)
				_ = conn.SetWriteDeadline(time.Now().Add(200 * time.Millisecond))
				if _, err := conn.Write(probe); err != nil {
					closedFast.Add(1)
					_ = conn.Close()
					continue
				}

				time.Sleep(*hold)
				_ = conn.Close()
			}
		}()
	}

	for i := 0; i < *total; i++ {
		jobs <- struct{}{}
	}
	close(jobs)
	wg.Wait()

	elapsed := time.Since(start)
	fmt.Printf("elapsed=%s\n", elapsed)
	fmt.Printf("dial-success=%d dial-failed=%d fast-closed=%d\n",
		success.Load(), dialFailed.Load(), closedFast.Load())
	fmt.Println("done")
}
