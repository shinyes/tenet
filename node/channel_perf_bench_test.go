package node

import (
	"bytes"
	"testing"
)

func newChannelBenchNode(b *testing.B, channels []string) *Node {
	b.Helper()
	opts := []Option{
		WithNetworkPassword("bench-secret"),
	}
	for _, ch := range channels {
		opts = append(opts, WithChannelID(ch))
	}
	n, err := NewNode(opts...)
	if err != nil {
		b.Fatalf("new node failed: %v", err)
	}
	return n
}

func BenchmarkIsChannelSubscribed_Hit(b *testing.B) {
	n := newChannelBenchNode(b, []string{"ops", "chat", "metrics", "control"})
	h := hashChannelName("ops")
	buf := h[:]

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !n.isChannelSubscribed(buf) {
			b.Fatal("expected subscribed")
		}
	}
}

func BenchmarkIsChannelSubscribed_Miss(b *testing.B) {
	n := newChannelBenchNode(b, []string{"ops", "chat", "metrics", "control"})
	h := hashChannelName("unknown")
	buf := h[:]

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if n.isChannelSubscribed(buf) {
			b.Fatal("expected not subscribed")
		}
	}
}

func BenchmarkHashChannelName(b *testing.B) {
	name := "very-hot-channel-name-for-benchmark"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = hashChannelName(name)
	}
}

// linearChannelSubscribed simulates the old implementation:
// scan all local channels and hash each one for comparison.
func linearChannelSubscribed(channels []string, channelHash []byte) bool {
	for _, ch := range channels {
		h := hashChannelName(ch)
		if bytes.Equal(h[:], channelHash) {
			return true
		}
	}
	return false
}

func BenchmarkIsChannelSubscribed_LinearHit(b *testing.B) {
	channels := []string{"ops", "chat", "metrics", "control"}
	h := hashChannelName("ops")
	buf := h[:]

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !linearChannelSubscribed(channels, buf) {
			b.Fatal("expected subscribed")
		}
	}
}

func BenchmarkIsChannelSubscribed_LinearMiss(b *testing.B) {
	channels := []string{"ops", "chat", "metrics", "control"}
	h := hashChannelName("unknown")
	buf := h[:]

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if linearChannelSubscribed(channels, buf) {
			b.Fatal("expected not subscribed")
		}
	}
}
