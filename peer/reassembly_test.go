package peer

import (
	"sync"
	"testing"
)

func TestReassemblyLifecycle(t *testing.T) {
	p := &Peer{}

	p.StartReassembly([]byte("a"))
	ok, overflow := p.AppendReassembly([]byte("b"), 16)
	if !ok || overflow {
		t.Fatalf("append failed unexpectedly: ok=%v overflow=%v", ok, overflow)
	}

	complete, ok, overflow := p.FinishReassembly([]byte("c"), 16)
	if !ok || overflow {
		t.Fatalf("finish failed unexpectedly: ok=%v overflow=%v", ok, overflow)
	}
	if string(complete) != "abc" {
		t.Fatalf("unexpected reassembly result: %q", string(complete))
	}

	_, ok, overflow = p.FinishReassembly([]byte("d"), 16)
	if ok || overflow {
		t.Fatalf("finish without buffer should be ignored: ok=%v overflow=%v", ok, overflow)
	}
}

func TestReassemblyOverflowResetsBuffer(t *testing.T) {
	p := &Peer{}

	p.StartReassembly([]byte("12345"))
	ok, overflow := p.AppendReassembly([]byte("67"), 6)
	if ok || !overflow {
		t.Fatalf("expected overflow on append: ok=%v overflow=%v", ok, overflow)
	}

	_, ok, overflow = p.FinishReassembly([]byte("8"), 6)
	if ok || overflow {
		t.Fatalf("finish after overflow should be ignored: ok=%v overflow=%v", ok, overflow)
	}
}

func TestReassemblyConcurrentAppend(t *testing.T) {
	p := &Peer{}
	p.StartReassembly([]byte("x"))

	const n = 32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			ok, overflow := p.AppendReassembly([]byte("y"), 1<<20)
			if !ok || overflow {
				t.Errorf("append failed: ok=%v overflow=%v", ok, overflow)
			}
		}()
	}
	wg.Wait()

	complete, ok, overflow := p.FinishReassembly([]byte("z"), 1<<20)
	if !ok || overflow {
		t.Fatalf("finish failed unexpectedly: ok=%v overflow=%v", ok, overflow)
	}
	if len(complete) != 1+n+1 {
		t.Fatalf("unexpected length: got=%d want=%d", len(complete), 1+n+1)
	}
}
