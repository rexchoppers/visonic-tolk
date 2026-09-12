package tolktest

import (
	"bufio"
	"bytes"
	"testing"
	"time"

	"github.com/rexchoppers/visonic-tolk/internal/message"
	"github.com/rexchoppers/visonic-tolk/internal/powerlink31"
	"github.com/rexchoppers/visonic-tolk/internal/tolk"
)

func ack(msgID int) []byte {
	return powerlink31.Frame{
		Type:    powerlink31.TypeAck,
		MsgID:   msgID,
		Account: account,
		Panel:   panelID,
		Data:    message.PLAck,
	}.Encode()
}

// Nothing released the gate once, so every message to the panel waited the
// full five seconds and an eprom download could never finish.
func TestAnAcknowledgedMessageLetsTheNextOneStraightOut(t *testing.T) {
	cfg := startWith(t, tolk.Config{
		VisonicAddr: "192.0.2.1:5001",
		Reconnect:   time.Hour,
	})

	ha := dial(t, cfg.MonitorAddr)
	pan := dial(t, cfg.PanelAddr)

	time.Sleep(100 * time.Millisecond)

	// Speak once so tolk learns the addressing to answer with.
	pan.Write(frame(1, []byte{0x0d, 0xb0, 0x03, 0x18, 0x0d, 0x0a}).Encode())
	expect(t, ha, []byte{0x0d, 0xb0, 0x03, 0x18, 0x0d, 0x0a})

	pan.SetReadDeadline(time.Now().Add(10 * time.Second))
	s := bufio.NewScanner(pan)
	s.Split(powerlink31.SplitFrames)

	first := []byte{0xa2, 0x00, 0x00, 0x08, 0x00, 0x00, 0x43}
	second := []byte{0xa6, 0x00, 0x00, 0x01, 0x00, 0x00, 0x43}

	ha.Write(message.Wrap(first))
	ha.Write(message.Wrap(second))

	got := nextFrom(t, s, message.Wrap(first))
	if got == nil {
		t.Fatal("the panel never got the first message")
	}

	pan.Write(ack(got.MsgID))
	started := time.Now()

	if nextFrom(t, s, message.Wrap(second)) == nil {
		t.Fatal("the panel never got the second message")
	}

	if waited := time.Since(started); waited > 2*time.Second {
		t.Errorf("second message took %v after the ack, want it straight out", waited)
	}
}

// nextFrom reads frames until the payload matches, skipping tolk's own traffic.
func nextFrom(t *testing.T, s *bufio.Scanner, want []byte) *powerlink31.Frame {
	t.Helper()

	for s.Scan() {
		f, err := powerlink31.Decode(s.Bytes())
		if err != nil {
			continue
		}
		if bytes.Equal(f.Data, want) {
			return &f
		}
	}
	return nil
}
