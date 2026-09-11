package messagetest

import (
	"bytes"
	"errors"
	"testing"

	"github.com/rexchoppers/visonic-tolk/internal/message"
)

func TestChecksumMatchesPython(t *testing.T) {
	for _, v := range pythonChecksums {
		if got := message.Checksum(v.body); got != v.sum {
			t.Errorf("Checksum(%x) = %#02x, python gives %#02x", v.body, got, v.sum)
		}
	}
}

func TestWrapMatchesPython(t *testing.T) {
	for _, v := range pythonChecksums {
		if got := message.Wrap(v.body); !bytes.Equal(got, v.wrap) {
			t.Errorf("Wrap(%x) = %x, python gives %x", v.body, got, v.wrap)
		}
	}
}

func TestManagedMessagesAreSelfConsistent(t *testing.T) {
	cases := map[string]struct {
		body []byte
		want []byte
	}{
		"keepalive":     {[]byte{0xb0, 0x01, 0x6a, 0x00, 0x43}, message.Keepalive},
		"powerlink ack": {[]byte{0x02, 0x43}, message.PLAck},
		"ack":           {[]byte{0x02}, message.Ack},
	}

	for name, c := range cases {
		if got := message.Wrap(c.body); !bytes.Equal(got, c.want) {
			t.Errorf("%s: Wrap(%x) = %x, constant is %x", name, c.body, got, c.want)
		}
	}
}

func TestFromMonitorPassesFullMessagesThrough(t *testing.T) {
	in := []byte{0x0d, 0xb0, 0x01, 0x6a, 0x00, 0x43, 0xa0, 0x0a}

	got, isAck, err := message.FromMonitor(in)
	if err != nil {
		t.Fatalf("FromMonitor: %v", err)
	}
	if isAck {
		t.Error("reported an ack, want a normal message")
	}
	if !bytes.Equal(got, in) {
		t.Errorf("got %x, want it unchanged", got)
	}
}

func TestFromMonitorSwapsTheAck(t *testing.T) {
	got, isAck, err := message.FromMonitor(message.Ack)
	if err != nil {
		t.Fatalf("FromMonitor: %v", err)
	}
	if !isAck {
		t.Error("did not report an ack")
	}
	if !bytes.Equal(got, message.PLAck) {
		t.Errorf("got %x, want the powerlink ack %x", got, message.PLAck)
	}
}

func TestFromMonitorWrapsABareBody(t *testing.T) {
	in := []byte{0xa2, 0x00, 0x00, 0x08, 0x00, 0x00, 0x43}

	got, isAck, err := message.FromMonitor(in)
	if err != nil {
		t.Fatalf("FromMonitor: %v", err)
	}
	if isAck {
		t.Error("reported an ack, want a normal message")
	}
	if !bytes.Equal(got, message.Wrap(in)) {
		t.Errorf("got %x, want it wrapped", got)
	}
}

func TestFromMonitorRefusesShorthand(t *testing.T) {
	if _, _, err := message.FromMonitor([]byte{0xb0, 0x6a}); !errors.Is(err, message.ErrShorthand) {
		t.Errorf("err = %v, want ErrShorthand", err)
	}
}

func TestFromMonitorRefusesEmpty(t *testing.T) {
	if _, _, err := message.FromMonitor(nil); !errors.Is(err, message.ErrEmpty) {
		t.Errorf("err = %v, want ErrEmpty", err)
	}
}
