package tolktest

import (
	"bufio"
	"bytes"
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/rexchoppers/visonic-tolk/internal/message"
	"github.com/rexchoppers/visonic-tolk/internal/powerlink31"
	"github.com/rexchoppers/visonic-tolk/internal/tolk"
)

// waitForKeepalive reads frames until one carries a keepalive, or gives up.
func waitForKeepalive(t *testing.T, c net.Conn, within time.Duration) bool {
	t.Helper()

	c.SetReadDeadline(time.Now().Add(within))

	s := bufio.NewScanner(c)
	s.Split(powerlink31.SplitFrames)

	for s.Scan() {
		f, err := powerlink31.Decode(s.Bytes())
		if err != nil {
			continue
		}
		if bytes.Equal(f.Data, message.Keepalive) {
			return true
		}
	}
	return false
}

func startWith(t *testing.T, cfg tolk.Config) tolk.Config {
	t.Helper()

	cfg.PanelAddr = freeAddr(t)
	cfg.MonitorAddr = freeAddr(t)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	go tolk.New(cfg, slog.New(slog.DiscardHandler)).Run(ctx)
	return cfg
}

func TestAQuietPanelIsPokedWhenTheCloudIsDown(t *testing.T) {
	cfg := startWith(t, tolk.Config{
		VisonicAddr: "192.0.2.1:5001",
		Reconnect:   time.Hour,
		Keepalive:   100 * time.Millisecond,
	})

	pan := dial(t, cfg.PanelAddr)

	// Speak once so tolk learns the addressing, then go quiet.
	pan.Write(frame(1, []byte{0x0d, 0xb0, 0x03, 0x18, 0x0d, 0x0a}).Encode())

	if !waitForKeepalive(t, pan, 3*time.Second) {
		t.Error("panel was never poked")
	}
}

func TestABusyPanelIsLeftAlone(t *testing.T) {
	cfg := startWith(t, tolk.Config{
		VisonicAddr: "192.0.2.1:5001",
		Reconnect:   time.Hour,
		Keepalive:   400 * time.Millisecond,
	})

	pan := dial(t, cfg.PanelAddr)

	stop := make(chan struct{})
	go func() {
		id := 1
		for {
			select {
			case <-stop:
				return
			default:
				pan.Write(frame(id, []byte{0x0d, 0xb0, 0x03, 0x18, 0x0d, 0x0a}).Encode())
				id++
				time.Sleep(50 * time.Millisecond)
			}
		}
	}()

	poked := waitForKeepalive(t, pan, time.Second)
	close(stop)

	if poked {
		t.Error("a panel that keeps talking was poked anyway")
	}
}

func TestKeepaliveOffWhenNotConfigured(t *testing.T) {
	cfg := startWith(t, tolk.Config{
		VisonicAddr: "192.0.2.1:5001",
		Reconnect:   time.Hour,
	})

	pan := dial(t, cfg.PanelAddr)
	pan.Write(frame(1, []byte{0x0d, 0xb0, 0x03, 0x18, 0x0d, 0x0a}).Encode())

	if waitForKeepalive(t, pan, 500*time.Millisecond) {
		t.Error("poked with no keepalive configured")
	}
}
