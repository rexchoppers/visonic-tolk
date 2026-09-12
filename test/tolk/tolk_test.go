package tolktest

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"github.com/rexchoppers/visonic-tolk/internal/message"
	"github.com/rexchoppers/visonic-tolk/internal/powerlink31"
	"github.com/rexchoppers/visonic-tolk/internal/tolk"
)

const (
	account = "001234"
	panelID = "2A4CC3"
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

// cloud stands in for the Visonic servers.
func cloud(t *testing.T) (addr string, accepted chan net.Conn) {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { l.Close() })

	accepted = make(chan net.Conn, 4)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			accepted <- c
		}
	}()
	return l.Addr().String(), accepted
}

func dial(t *testing.T, addr string) net.Conn {
	t.Helper()

	for range 50 {
		if c, err := net.Dial("tcp", addr); err == nil {
			t.Cleanup(func() { c.Close() })
			return c
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s never became reachable", addr)
	return nil
}

// expect waits for want to show up. Status messages arrive unprompted, so a
// reader here cannot assume the next bytes are the ones it asked for.
func expect(t *testing.T, c net.Conn, want []byte) {
	t.Helper()

	var seen []byte
	deadline := time.Now().Add(3 * time.Second)

	for time.Now().Before(deadline) {
		c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))

		buf := make([]byte, 1024)
		n, err := c.Read(buf)
		if n > 0 {
			seen = append(seen, buf[:n]...)
			if bytes.Contains(seen, want) {
				return
			}
		}
		if err != nil && !errors.Is(err, os.ErrDeadlineExceeded) {
			break
		}
	}

	t.Errorf("\nnever saw %q\n      got %q", want, seen)
}

func start(t *testing.T) (panelAddr, monitorAddr string, cloudConns chan net.Conn) {
	t.Helper()

	cloudAddr, cloudConns := cloud(t)
	panelAddr, monitorAddr = freeAddr(t), freeAddr(t)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	go tolk.New(tolk.Config{
		PanelAddr:   panelAddr,
		MonitorAddr: monitorAddr,
		VisonicAddr: cloudAddr,
		Reconnect:   20 * time.Millisecond,
	}, slog.New(slog.DiscardHandler)).Run(ctx)

	return panelAddr, monitorAddr, cloudConns
}

func frame(msgID int, data []byte) powerlink31.Frame {
	return powerlink31.Frame{
		Type:    powerlink31.TypeBBA,
		MsgID:   msgID,
		Account: account,
		Panel:   panelID,
		Data:    data,
	}
}

func TestPanelMessageReachesHomeAssistantAsAPayloadAndTheCloudAsAFrame(t *testing.T) {
	panelAddr, monitorAddr, cloudConns := start(t)

	ha := dial(t, monitorAddr)
	pan := dial(t, panelAddr)

	var cloudConn net.Conn
	select {
	case cloudConn = <-cloudConns:
	case <-time.After(2 * time.Second):
		t.Fatal("tolk never dialled the cloud")
	}

	// Let tolk record that both are up before the frame arrives.
	time.Sleep(100 * time.Millisecond)

	f := frame(1, []byte{0x0d, 0xb0, 0x03, 0x18, 0x0d, 0x0a})
	pan.Write(f.Encode())

	expect(t, ha, f.Data)
	expect(t, cloudConn, f.Encode())
}

func TestHomeAssistantMessageReachesThePanelAsAFrame(t *testing.T) {
	panelAddr, monitorAddr, cloudConns := start(t)

	ha := dial(t, monitorAddr)
	pan := dial(t, panelAddr)

	select {
	case <-cloudConns:
	case <-time.After(2 * time.Second):
		t.Fatal("tolk never dialled the cloud")
	}

	time.Sleep(100 * time.Millisecond)

	// The panel speaks first so tolk learns the addressing to answer with.
	pan.Write(frame(1, []byte{0x0d, 0xb0, 0x03, 0x18, 0x0d, 0x0a}).Encode())
	expect(t, ha, []byte{0x0d, 0xb0, 0x03, 0x18, 0x0d, 0x0a})

	body := []byte{0xa2, 0x00, 0x00, 0x08, 0x00, 0x00, 0x43}
	ha.Write(body)

	got := waitForFrame(t, pan, func(f powerlink31.Frame) bool {
		return bytes.Equal(f.Data, message.Wrap(body))
	})
	if got == nil {
		t.Fatal("the panel never got the wrapped message")
	}
	if got.Account != account || got.Panel != panelID {
		t.Errorf("addressed %s/%s, want %s/%s", got.Account, got.Panel, account, panelID)
	}
}

// waitForFrame reads frames off the panel side until one matches, since tolk
// also sends acknowledgements the caller did not ask about.
func waitForFrame(t *testing.T, c net.Conn, match func(powerlink31.Frame) bool) *powerlink31.Frame {
	t.Helper()

	c.SetReadDeadline(time.Now().Add(3 * time.Second))

	s := bufio.NewScanner(c)
	s.Split(powerlink31.SplitFrames)

	for s.Scan() {
		f, err := powerlink31.Decode(s.Bytes())
		if err != nil {
			continue
		}
		if match(f) {
			return &f
		}
	}
	return nil
}

func TestPanelMessageStillReachesHomeAssistantWithNoCloud(t *testing.T) {
	cloudAddr := freeAddr(t)
	panelAddr, monitorAddr := freeAddr(t), freeAddr(t)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	go tolk.New(tolk.Config{
		PanelAddr:   panelAddr,
		MonitorAddr: monitorAddr,
		VisonicAddr: cloudAddr,
		Reconnect:   time.Hour,
	}, slog.New(slog.DiscardHandler)).Run(ctx)

	ha := dial(t, monitorAddr)
	pan := dial(t, panelAddr)

	time.Sleep(100 * time.Millisecond)

	f := frame(1, []byte{0x0d, 0xb0, 0x03, 0x18, 0x0d, 0x0a})
	pan.Write(f.Encode())

	expect(t, ha, f.Data)
}
