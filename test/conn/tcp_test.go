package conntest

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rexchoppers/visonic-tolk/internal/conn"
)

func freeAddr(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func TestListenHandsOverConnections(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	addr := freeAddr(t)
	got := make(chan net.Conn, 2)

	go conn.Listen(ctx, addr, discard(), func(c net.Conn) { got <- c })

	var dialed net.Conn
	for range 20 {
		var err error
		if dialed, err = net.Dial("tcp", addr); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if dialed == nil {
		t.Fatal("never became reachable")
	}
	defer dialed.Close()

	select {
	case c := <-got:
		c.Close()
	case <-time.After(time.Second):
		t.Fatal("connection was never handed over")
	}
}

func TestListenStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())

	addr := freeAddr(t)
	done := make(chan error, 1)
	go func() { done <- conn.Listen(ctx, addr, discard(), func(net.Conn) {}) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("err = %v, want nil on cancel", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Listen did not return")
	}
}

func TestDialReconnectsAfterTheFarEndGoesAway(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()

	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	var uses atomic.Int32
	go conn.Dial(ctx, l.Addr().String(), 10*time.Millisecond, discard(), func(c net.Conn) {
		uses.Add(1)
		io := make([]byte, 1)
		c.Read(io)
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if uses.Load() >= 3 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("connected %d times, want it to keep reconnecting", uses.Load())
}

func TestDialKeepsRetryingWhenNothingIsListening(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan struct{})
	go func() {
		conn.Dial(ctx, freeAddr(t), 10*time.Millisecond, discard(), func(net.Conn) {})
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)

	select {
	case <-done:
		t.Fatal("Dial gave up, want it still retrying")
	default:
	}

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Dial did not stop on cancel")
	}
}
