package tolktest

import (
	"testing"
	"time"

	"github.com/rexchoppers/visonic-tolk/internal/tolk"
)

const atDownload = 7

// b0Status builds a b0 0f panel state message, where byte 13 is the state and
// 7 means the panel is downloading.
func b0Status(state byte) []byte {
	m := []byte{
		0x0d, 0xb0, 0x03, 0x0f, 0x0b, 0x19, 0x07, 0x0f,
		0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x80, 0x96,
		0x43, 0xa6, 0x0a,
	}
	m[13] = state
	return m
}

func TestThePanelEnteringDownloadModeReachesHomeAssistant(t *testing.T) {
	cfg := startWith(t, tolk.Config{
		VisonicAddr: "192.0.2.1:5001",
		Reconnect:   time.Hour,
	})

	ha := dial(t, cfg.MonitorAddr)
	pan := dial(t, cfg.PanelAddr)

	time.Sleep(100 * time.Millisecond)

	pan.Write(frame(1, b0Status(7)).Encode())

	if waitForStatus(t, ha, func(s []byte) bool { return s[atDownload] == 1 }) == nil {
		t.Fatal("home assistant was never told the panel is downloading")
	}

	pan.Write(frame(2, b0Status(0)).Encode())

	if waitForStatus(t, ha, func(s []byte) bool { return s[atDownload] == 0 }) == nil {
		t.Error("home assistant was never told the panel had finished")
	}
}

func TestAnOrdinaryPanelMessageDoesNotTouchDownloadMode(t *testing.T) {
	cfg := startWith(t, tolk.Config{
		VisonicAddr: "192.0.2.1:5001",
		Reconnect:   time.Hour,
	})

	ha := dial(t, cfg.MonitorAddr)
	pan := dial(t, cfg.PanelAddr)

	time.Sleep(100 * time.Millisecond)

	pan.Write(frame(1, b0Status(7)).Encode())
	if waitForStatus(t, ha, func(s []byte) bool { return s[atDownload] == 1 }) == nil {
		t.Fatal("never entered download mode")
	}

	// A b0 carrying no panel state must leave the flag alone.
	pan.Write(frame(2, []byte{0x0d, 0xb0, 0x03, 0x18, 0x0d, 0x0a}).Encode())
	ha.Write([]byte{0x0d, 0xe1, 0x01, 0x00, 0x43, 0xd9, 0x0a})

	if waitForStatus(t, ha, func(s []byte) bool { return s[atDownload] == 1 }) == nil {
		t.Error("an unrelated message cleared download mode")
	}
}
