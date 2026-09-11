package conntest

import (
	"bufio"
	"bytes"
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/rexchoppers/visonic-tolk/internal/conn"
	"github.com/rexchoppers/visonic-tolk/internal/powerlink31"
)

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

// pair returns two ends of a real loopback connection.
func pair(t *testing.T) (near, far net.Conn) {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()

	done := make(chan net.Conn, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			done <- nil
			return
		}
		done <- c
	}()

	near, err = net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	far = <-done
	if far == nil {
		t.Fatal("accept failed")
	}

	t.Cleanup(func() {
		near.Close()
		far.Close()
	})
	return near, far
}

func read(t *testing.T, c net.Conn, n int, within time.Duration) []byte {
	t.Helper()

	c.SetReadDeadline(time.Now().Add(within))
	buf := make([]byte, n)
	got, err := c.Read(buf)
	if err != nil {
		return nil
	}
	return buf[:got]
}

func frame(msgID int, data []byte) []byte {
	return powerlink31.Frame{
		Type:    powerlink31.TypeBBA,
		MsgID:   msgID,
		Account: "001234",
		Panel:   "2A4CC3",
		Data:    data,
	}.Encode()
}

func run(t *testing.T, c *conn.Conn, onFrame func([]byte)) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	if onFrame == nil {
		onFrame = func([]byte) {}
	}
	go c.Run(ctx, onFrame)
}

func TestSendWritesToTheWire(t *testing.T) {
	near, far := pair(t)

	c := conn.New("panel", near, powerlink31.SplitFrames, discard())
	run(t, c, nil)

	want := frame(1, []byte{0x0d, 0xb0, 0x0a})
	c.Send(want, false)

	if got := read(t, far, len(want), time.Second); !bytes.Equal(got, want) {
		t.Errorf("wire got %q, want %q", got, want)
	}
}

func TestReceiveDeliversWholeFrames(t *testing.T) {
	near, far := pair(t)

	got := make(chan []byte, 4)
	c := conn.New("panel", near, powerlink31.SplitFrames, discard())
	run(t, c, func(b []byte) { got <- b })

	first := frame(1, []byte{0x0d, 0xb0, 0x0a})
	second := frame(2, []byte{0x0d, 0xa0, 0x0a})
	far.Write(append(bytes.Clone(first), second...))

	for _, want := range [][]byte{first, second} {
		select {
		case b := <-got:
			if !bytes.Equal(b, want) {
				t.Errorf("frame = %q, want %q", b, want)
			}
		case <-time.After(time.Second):
			t.Fatal("frame never arrived")
		}
	}
}

func TestGateHoldsTheNextSendUntilAcked(t *testing.T) {
	near, far := pair(t)

	c := conn.New("panel", near, powerlink31.SplitFrames, discard())
	c.AckWait = 10 * time.Second
	run(t, c, nil)

	first := frame(1, []byte{0x0d, 0xb0, 0x0a})
	second := frame(2, []byte{0x0d, 0xa0, 0x0a})

	c.Send(first, true)
	c.Send(second, false)

	if got := read(t, far, len(first), time.Second); !bytes.Equal(got, first) {
		t.Fatalf("first frame = %q, want %q", got, first)
	}

	if got := read(t, far, len(second), 200*time.Millisecond); got != nil {
		t.Fatalf("second frame arrived while the gate was shut: %q", got)
	}

	c.Ack()

	if got := read(t, far, len(second), time.Second); !bytes.Equal(got, second) {
		t.Errorf("second frame = %q, want %q after the ack", got, second)
	}
}

func TestGateReleasesOnTimeout(t *testing.T) {
	near, far := pair(t)

	c := conn.New("panel", near, powerlink31.SplitFrames, discard())
	c.AckWait = 50 * time.Millisecond
	run(t, c, nil)

	first := frame(1, []byte{0x0d, 0xb0, 0x0a})
	second := frame(2, []byte{0x0d, 0xa0, 0x0a})

	c.Send(first, true)
	c.Send(second, false)

	read(t, far, len(first), time.Second)

	if got := read(t, far, len(second), time.Second); !bytes.Equal(got, second) {
		t.Errorf("second frame = %q, want it sent anyway once the wait expired", got)
	}
}

// The whole reason for the rewrite: a peer stuck waiting for an
// acknowledgement must not stop any other peer sending.
func TestAStalledPeerDoesNotBlockAnother(t *testing.T) {
	stuckNear, stuckFar := pair(t)
	freeNear, freeFar := pair(t)

	stuck := conn.New("panel", stuckNear, powerlink31.SplitFrames, discard())
	stuck.AckWait = 10 * time.Second
	run(t, stuck, nil)

	free := conn.New("monitor", freeNear, powerlink31.SplitFrames, discard())
	run(t, free, nil)

	held := frame(1, []byte{0x0d, 0xb0, 0x0a})
	stuck.Send(held, true)
	stuck.Send(frame(2, []byte{0x0d, 0xa0, 0x0a}), false)
	read(t, stuckFar, len(held), time.Second)

	want := frame(3, []byte{0x0d, 0xe0, 0x0a})
	free.Send(want, false)

	if got := read(t, freeFar, len(want), time.Second); !bytes.Equal(got, want) {
		t.Errorf("free peer got %q, want %q while the other peer was stalled", got, want)
	}
}

func TestScannerSplitIsUsed(t *testing.T) {
	near, far := pair(t)

	got := make(chan int, 1)
	c := conn.New("panel", near, bufio.ScanLines, discard())
	run(t, c, func(b []byte) { got <- len(b) })

	far.Write([]byte("abc\n"))

	select {
	case n := <-got:
		if n != 3 {
			t.Errorf("token length %d, want 3", n)
		}
	case <-time.After(time.Second):
		t.Fatal("no token")
	}
}
