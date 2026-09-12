package tolktest

import (
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

func expect(t *testing.T, c net.Conn, want []byte) {
	t.Helper()

	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 512)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(buf[:n], want) {
		t.Errorf("\n got %q\nwant %q", buf[:n], want)
	}
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

	pan.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 512)
	n, err := pan.Read(buf)
	if err != nil {
		t.Fatalf("panel read: %v", err)
	}

	got, err := powerlink31.Decode(buf[:n])
	if err != nil {
		t.Fatalf("panel got something undecodable %q: %v", buf[:n], err)
	}
	if !bytes.Equal(got.Data, message.Wrap(body)) {
		t.Errorf("payload = %x, want %x", got.Data, message.Wrap(body))
	}
	if got.Account != account || got.Panel != panelID {
		t.Errorf("addressed %s/%s, want %s/%s", got.Account, got.Panel, account, panelID)
	}
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
