package powerlink31test

import (
	"bytes"
	"testing"

	"github.com/rexchoppers/visonic-tolk/internal/powerlink31"
)

const (
	account = "001234"
	panel   = "2A4CC3"
)

func frameFor(msgID int, data []byte, isAck bool) powerlink31.Frame {
	typ := powerlink31.TypeBBA
	if isAck {
		typ = powerlink31.TypeAck
	}
	return powerlink31.Frame{
		Type:    typ,
		MsgID:   msgID,
		Account: account,
		Panel:   panel,
		Data:    data,
	}
}

func TestEncodeMatchesPython(t *testing.T) {
	for _, v := range pythonFrames {
		got := frameFor(v.msgID, v.data, v.isAck).Encode()
		if !bytes.Equal(got, v.raw) {
			t.Errorf("msgID %d\n got %q\nwant %q", v.msgID, got, v.raw)
		}
	}
}

func TestDecodePythonFrames(t *testing.T) {
	for _, v := range pythonFrames {
		f, err := powerlink31.Decode(v.raw)
		if err != nil {
			t.Fatalf("msgID %d: %v", v.msgID, err)
		}
		if f.MsgID != v.msgID {
			t.Errorf("msgID = %d, want %d", f.MsgID, v.msgID)
		}
		if f.Account != account || f.Panel != panel {
			t.Errorf("address = %s/%s, want %s/%s", f.Account, f.Panel, account, panel)
		}
		if !bytes.Equal(f.Data, v.data) {
			t.Errorf("msgID %d data = %x, want %x", v.msgID, f.Data, v.data)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	for _, v := range pythonFrames {
		f, err := powerlink31.Decode(frameFor(v.msgID, v.data, v.isAck).Encode())
		if err != nil {
			t.Fatalf("msgID %d: %v", v.msgID, err)
		}
		if !bytes.Equal(f.Encode(), v.raw) {
			t.Errorf("msgID %d did not survive a round trip", v.msgID)
		}
	}
}

func TestDecodeNak(t *testing.T) {
	raw := []byte("\nE5630025\"NAK\"0000R0L0A0[]_10:10:18,07-30-2024\r")

	f, err := powerlink31.Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if f.Type != powerlink31.TypeNak {
		t.Errorf("type = %q, want %q", f.Type, powerlink31.TypeNak)
	}
	if string(f.Data) != "10:10:18,07-30-2024" {
		t.Errorf("data = %q, want the timestamp", f.Data)
	}
}

func TestDecodeRejectsRubbish(t *testing.T) {
	cases := map[string][]byte{
		"empty":         {},
		"no newline":    []byte("6BAF001D\"VIS-BBA\"0001L0#2A4CC3[\x0d]\r"),
		"no bracket":    []byte("\n6BAF001D\"VIS-BBA\"0001L0#2A4CC3\r"),
		"no quotes":     []byte("\n6BAF001DVIS-BBA0001L0#2A4CC3[\x0d]\r"),
		"truncated":     []byte("\n6BAF\r"),
		"no terminator": []byte("\n6BAF001D\"VIS-BBA\"0001L0#2A4CC3[\x0d]"),
	}

	for name, raw := range cases {
		if _, err := powerlink31.Decode(raw); err == nil {
			t.Errorf("%s: decoded without error, want a refusal", name)
		}
	}
}
