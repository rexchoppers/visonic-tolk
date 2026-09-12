package messagetest

import (
	"bufio"
	"bytes"
	"io"
	"testing"
	"testing/iotest"

	"github.com/rexchoppers/visonic-tolk/internal/message"
)

func scan(t *testing.T, stream []byte, slow bool) [][]byte {
	t.Helper()

	var src io.Reader = bytes.NewReader(stream)
	if slow {
		src = iotest.OneByteReader(src)
	}

	s := bufio.NewScanner(src)
	s.Split(message.Split)

	var got [][]byte
	for s.Scan() {
		got = append(got, bytes.Clone(s.Bytes()))
	}
	if err := s.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return got
}

// The bug this exists to fix: two messages written back to back arrive in one
// read, and the python keeps only the first.
func TestSplitSeparatesMessagesThatArrivedTogether(t *testing.T) {
	first := message.Wrap([]byte{0xe1, 0x02, 0x01, 0x43})
	second := message.Wrap([]byte{0xe1, 0x01, 0x00, 0x43})

	got := scan(t, append(bytes.Clone(first), second...), false)

	if len(got) != 2 {
		t.Fatalf("got %d messages, want 2", len(got))
	}
	if !bytes.Equal(got[0], first) || !bytes.Equal(got[1], second) {
		t.Errorf("got %x and %x, want %x and %x", got[0], got[1], first, second)
	}
}

func TestSplitSurvivesOneByteAtATime(t *testing.T) {
	var stream []byte
	want := [][]byte{
		message.Wrap([]byte{0xe1, 0x02, 0x01, 0x43}),
		message.Keepalive,
		message.PLAck,
	}
	for _, m := range want {
		stream = append(stream, m...)
	}

	got := scan(t, stream, true)

	if len(got) != len(want) {
		t.Fatalf("got %d messages, want %d", len(got), len(want))
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Errorf("message %d = %x, want %x", i, got[i], want[i])
		}
	}
}

func TestSplitKeepsTerminatorBytesInsideABody(t *testing.T) {
	// A body holding 0x0a, which a scan for the terminator would cut short.
	m := message.Wrap([]byte{0xb0, 0x0a, 0x0a, 0x0a, 0x43})

	got := scan(t, m, false)

	if len(got) != 1 {
		t.Fatalf("got %d messages, want 1", len(got))
	}
	if !bytes.Equal(got[0], m) {
		t.Errorf("got %x, want %x", got[0], m)
	}
}

func TestSplitPassesABareBodyThroughWhole(t *testing.T) {
	body := []byte{0xa2, 0x00, 0x00, 0x08, 0x00, 0x00, 0x43}

	got := scan(t, body, false)

	if len(got) != 1 || !bytes.Equal(got[0], body) {
		t.Errorf("got %x, want the bare body %x untouched", got, body)
	}
}
