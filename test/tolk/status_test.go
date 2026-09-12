package tolktest

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"github.com/rexchoppers/visonic-tolk/internal/message"
	"github.com/rexchoppers/visonic-tolk/internal/tolk"
)

// A status message is 0d e0 panels clouds monitors proxy stealth download 43
// checksum 0a, so eleven bytes.
const statusLen = 11

const (
	atPanels  = 2
	atStealth = 6
)

func startStealthy(t *testing.T, stealth time.Duration) (panelAddr, monitorAddr string, clouds chan net.Conn) {
	t.Helper()

	cloudAddr, clouds := cloud(t)
	panelAddr, monitorAddr = freeAddr(t), freeAddr(t)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	go tolk.New(tolk.Config{
		PanelAddr:      panelAddr,
		MonitorAddr:    monitorAddr,
		VisonicAddr:    cloudAddr,
		Reconnect:      20 * time.Millisecond,
		StealthTimeout: stealth,
	}, slog.New(slog.DiscardHandler)).Run(ctx)

	return panelAddr, monitorAddr, clouds
}

// waitForStatus reads until a status message satisfies ok.
func waitForStatus(t *testing.T, c net.Conn, ok func(status []byte) bool) []byte {
	t.Helper()

	var seen []byte
	deadline := time.Now().Add(3 * time.Second)

	for time.Now().Before(deadline) {
		c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))

		buf := make([]byte, 1024)
		n, err := c.Read(buf)
		if n > 0 {
			seen = append(seen, buf[:n]...)
			for i := 0; i+statusLen <= len(seen); i++ {
				if seen[i] != 0x0d || seen[i+1] != 0xe0 {
					continue
				}
				if s := seen[i : i+statusLen]; ok(s) {
					return s
				}
			}
		}
		if err != nil && !errors.Is(err, os.ErrDeadlineExceeded) {
			break
		}
	}

	t.Logf("saw %q", seen)
	return nil
}

func TestHomeAssistantIsToldWhatIsConnected(t *testing.T) {
	panelAddr, monitorAddr, _ := start(t)

	ha := dial(t, monitorAddr)

	if s := waitForStatus(t, ha, func(s []byte) bool { return s[atPanels] == 0 }); s == nil {
		t.Fatal("no status on connecting")
	}

	dial(t, panelAddr)

	if s := waitForStatus(t, ha, func(s []byte) bool { return s[atPanels] == 1 }); s == nil {
		t.Error("never told a panel had arrived")
	}
}

func TestHomeAssistantCanAskForStatus(t *testing.T) {
	_, monitorAddr, _ := start(t)

	ha := dial(t, monitorAddr)
	waitForStatus(t, ha, func([]byte) bool { return true })

	ha.Write(message.Wrap([]byte{0xe1, 0x01, 0x00, 0x43}))

	if s := waitForStatus(t, ha, func([]byte) bool { return true }); s == nil {
		t.Error("asking for status got nothing back")
	}
}

func TestStealthTakesTheCloudAwayAndGivesItBack(t *testing.T) {
	panelAddr, monitorAddr, clouds := startStealthy(t, 700*time.Millisecond)

	ha := dial(t, monitorAddr)
	dial(t, panelAddr)

	var first net.Conn
	select {
	case first = <-clouds:
	case <-time.After(2 * time.Second):
		t.Fatal("never dialled the cloud")
	}

	ha.Write(message.Wrap([]byte{0xe1, 0x02, 0x01, 0x43}))

	if s := waitForStatus(t, ha, func(s []byte) bool { return s[atStealth] == 1 }); s == nil {
		t.Fatal("never reported being in stealth")
	}

	first.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := first.Read(make([]byte, 1)); err == nil {
		t.Error("cloud link survived stealth, want it dropped")
	}

	select {
	case <-clouds:
	case <-time.After(4 * time.Second):
		t.Error("cloud never came back after stealth timed out")
	}
}

func TestStealthIsHeldWhileHomeAssistantKeepsAsking(t *testing.T) {
	_, monitorAddr, _ := startStealthy(t, 400*time.Millisecond)

	ha := dial(t, monitorAddr)
	ha.Write(message.Wrap([]byte{0xe1, 0x02, 0x01, 0x43}))

	if waitForStatus(t, ha, func(s []byte) bool { return s[atStealth] == 1 }) == nil {
		t.Fatal("never entered stealth")
	}

	for range 6 {
		time.Sleep(100 * time.Millisecond)
		ha.Write(message.Wrap([]byte{0xe1, 0x02, 0x01, 0x43}))
	}

	ha.Write(message.Wrap([]byte{0xe1, 0x01, 0x00, 0x43}))

	if waitForStatus(t, ha, func(s []byte) bool { return s[atStealth] == 1 }) == nil {
		t.Error("stealth lapsed while it was still being asked for")
	}
}
