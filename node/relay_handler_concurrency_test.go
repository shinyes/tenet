package node

import (
	"fmt"
	"net"
	"sync"
	"testing"

	"github.com/shinyes/tenet/nat"
)

func TestRegisterRelayCandidateConcurrentAccess(t *testing.T) {
	n := newDiscoveryTestNode(t)
	n.relayManager = nat.NewRelayManager(nil)

	const workers = 16
	const perWorker = 64

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		w := w
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				addr := &net.UDPAddr{
					IP:   net.IPv4(127, 0, 0, 1),
					Port: 20000 + w*perWorker + i,
				}
				n.registerRelayCandidate(fmt.Sprintf("peer-%d-%d", w, i), addr)
			}
		}()
	}
	wg.Wait()

	want := workers * perWorker
	n.mu.RLock()
	got := len(n.relayAddrSet)
	n.mu.RUnlock()
	if got != want {
		t.Fatalf("unexpected relayAddrSet size: got %d, want %d", got, want)
	}
}
