package conntest

import (
	"testing"
	"time"

	"github.com/rexchoppers/visonic-tolk/internal/conn"
	"github.com/rexchoppers/visonic-tolk/internal/message"
	"github.com/rexchoppers/visonic-tolk/internal/powerlink31"
)

func TestWatchdogDropsAQuietPeer(t *testing.T) {
	near, far := pair(t)

	c := conn.New("panel", near, powerlink31.SplitFrames, discard())
	c.Watchdog = 100 * time.Millisecond
	run(t, c, nil)

	far.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := far.Read(make([]byte, 1)); err == nil {
		t.Fatal("read succeeded, want the connection dropped")
	}
}

func TestWatchdogLeavesABusyPeerAlone(t *testing.T) {
	near, far := pair(t)

	c := conn.New("panel", near, powerlink31.SplitFrames, discard())
	c.Watchdog = 300 * time.Millisecond
	run(t, c, nil)

	stop := make(chan struct{})
	defer close(stop)

	go func() {
		f := frame(1, []byte{0x0d, 0xb0, 0x0a})
		for {
			select {
			case <-stop:
				return
			default:
				far.Write(f)
				time.Sleep(30 * time.Millisecond)
			}
		}
	}()

	time.Sleep(700 * time.Millisecond)

	if idle := c.Idle(); idle > 300*time.Millisecond {
		t.Errorf("idle %v, want the traffic to have kept it alive", idle)
	}
}

func TestNoWatchdogMeansNoDrop(t *testing.T) {
	near, far := pair(t)

	c := conn.New("monitor", near, message.Split, discard())
	run(t, c, nil)

	time.Sleep(200 * time.Millisecond)

	if _, err := far.Write([]byte{0x0d, 0x02, 0xfd, 0x0a}); err != nil {
		t.Errorf("write failed, want the connection still up: %v", err)
	}
}

func TestIdleStartsAtZero(t *testing.T) {
	near, _ := pair(t)

	c := conn.New("panel", near, powerlink31.SplitFrames, discard())
	run(t, c, nil)

	time.Sleep(50 * time.Millisecond)

	if idle := c.Idle(); idle > time.Second {
		t.Errorf("idle %v straight after starting, want it near zero", idle)
	}
}
