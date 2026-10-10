package tolktest

import (
	"bufio"
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/rexchoppers/visonic-tolk/internal/message"
	"github.com/rexchoppers/visonic-tolk/internal/powerlink31"
	"github.com/rexchoppers/visonic-tolk/internal/tolk"
)

var timeout = []byte{0x0d, 0x06, 0xf9, 0x0a}

func isInit(asks []byte) func(powerlink31.Frame) bool {
	return func(f powerlink31.Frame) bool { return bytes.Equal(f.Data, message.Init(asks)) }
}

func nextCloud(t *testing.T, clouds chan net.Conn) net.Conn {
	t.Helper()

	select {
	case c := <-clouds:
		return c
	case <-time.After(2 * time.Second):
		t.Fatal("tolk never dialled the cloud")
		return nil
	}
}

// The python skips the first cloud link after it starts and greets every one
// after that. The cloud is held back here so the panel has spoken before the
// first link comes up, which is the only way the first can be told apart.
func TestOnlyAReturningCloudIsGreeted(t *testing.T) {
	cloudAddr := freeAddr(t)
	cfg := startWith(t, tolk.Config{VisonicAddr: cloudAddr, Reconnect: 50 * time.Millisecond})

	pan := dial(t, cfg.PanelAddr)
	pan.Write(frame(1, timeout).Encode())
	time.Sleep(100 * time.Millisecond)

	l, err := net.Listen("tcp", cloudAddr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { l.Close() })

	first, err := l.Accept()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}

	pan.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	s := bufio.NewScanner(pan)
	s.Split(powerlink31.SplitFrames)
	for s.Scan() {
		if f, err := powerlink31.Decode(s.Bytes()); err == nil && isInit(nil)(f) {
			t.Fatal("the first cloud link was greeted, want it left alone")
		}
	}

	first.Close()
	if _, err := l.Accept(); err != nil {
		t.Fatalf("accept: %v", err)
	}

	if waitForFrame(t, pan, isInit(nil)) == nil {
		t.Fatal("the returning cloud link was never greeted")
	}
}

// The python waits on the panel's ack for its own message and keeps it, so
// Visonic never sees an ack for a message it did not send.
func TestThePanelsAckForTheGreetingStaysWithTolk(t *testing.T) {
	panelAddr, _, clouds := start(t)
	pan := dial(t, panelAddr)

	first := nextCloud(t, clouds)
	pan.Write(frame(1, timeout).Encode())
	expect(t, first, timeout)

	first.Close()
	second := nextCloud(t, clouds)

	greeting := waitForFrame(t, pan, isInit(nil))
	if greeting == nil {
		t.Fatal("the returning cloud link was never greeted")
	}

	pan.Write(ack(greeting.MsgID))
	after := []byte{0x0d, 0xb0, 0x03, 0x18, 0x0d, 0x0a}
	pan.Write(frame(2, after).Encode())

	second.SetReadDeadline(time.Now().Add(3 * time.Second))
	s := bufio.NewScanner(second)
	s.Split(powerlink31.SplitFrames)
	for s.Scan() {
		f, err := powerlink31.Decode(s.Bytes())
		if err != nil {
			continue
		}
		if f.Type == powerlink31.TypeAck && f.MsgID == greeting.MsgID {
			t.Fatal("the panel's ack for tolk's greeting reached the cloud")
		}
		if bytes.Equal(f.Data, after) {
			return
		}
	}
	t.Fatal("the cloud never got the panel's next message")
}

// While the cloud is down the panel announces what it has to tell in a b0 51.
// The python keeps that list and asks for it when the cloud comes back.
func TestAsksMadeWhileTheCloudIsDownAreRequestedOnReturn(t *testing.T) {
	cloudAddr, clouds := cloud(t)
	cfg := startWith(t, tolk.Config{VisonicAddr: cloudAddr, Reconnect: 300 * time.Millisecond})

	pan := dial(t, cfg.PanelAddr)
	first := nextCloud(t, clouds)
	pan.Write(frame(1, timeout).Encode())
	expect(t, first, timeout)

	first.Close()
	time.Sleep(50 * time.Millisecond)

	asks := []byte{0x0d, 0xb0, 0x03, 0x51, 0x08, 0xff, 0x08, 0xff, 0x03, 0x18, 0x24, 0x4b, 0x20, 0x43, 0xfc, 0x0a}
	pan.Write(frame(2, asks).Encode())

	nextCloud(t, clouds)

	if waitForFrame(t, pan, isInit([]byte{0x18, 0x24, 0x4b})) == nil {
		t.Fatal("the panel was never asked for what it announced")
	}
}

// The python counts leaving stealth as the end of starting up, so a first
// cloud link that stealth held back is greeted like a returning one.
func TestAFirstCloudHeldBackByStealthIsGreeted(t *testing.T) {
	panelAddr, monitorAddr, clouds := startStealthy(t, time.Minute)

	ha := dial(t, monitorAddr)
	ha.Write(message.Wrap([]byte{0xe1, 0x02, 0x01, 0x43}))
	if waitForStatus(t, ha, func(s []byte) bool { return s[atStealth] == 1 }) == nil {
		t.Fatal("never reported being in stealth")
	}

	pan := dial(t, panelAddr)
	pan.Write(frame(1, timeout).Encode())
	time.Sleep(100 * time.Millisecond)

	ha.Write(message.Wrap([]byte{0xe1, 0x02, 0x00, 0x43}))
	nextCloud(t, clouds)

	if waitForFrame(t, pan, isInit(nil)) == nil {
		t.Fatal("the first cloud link after stealth was never greeted")
	}
}
