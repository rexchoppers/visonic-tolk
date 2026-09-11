package powerlink31test

import (
	"bufio"
	"bytes"
	"io"
	"testing"
	"testing/iotest"

	"github.com/rexchoppers/visonic-tolk/internal/powerlink31"
)

func scanAll(t *testing.T, stream []byte, slow bool) [][]byte {
	t.Helper()

	var src io.Reader = bytes.NewReader(stream)
	if slow {
		src = iotest.OneByteReader(src)
	}

	s := bufio.NewScanner(src)
	s.Split(powerlink31.SplitFrames)

	var got [][]byte
	for s.Scan() {
		got = append(got, bytes.Clone(s.Bytes()))
	}
	if err := s.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return got
}

func TestSplitWholeFrames(t *testing.T) {
	var stream []byte
	for _, v := range pythonFrames {
		stream = append(stream, v.raw...)
	}

	got := scanAll(t, stream, false)
	if len(got) != len(pythonFrames) {
		t.Fatalf("got %d frames, want %d", len(got), len(pythonFrames))
	}
	for i, v := range pythonFrames {
		if !bytes.Equal(got[i], v.raw) {
			t.Errorf("frame %d = %q, want %q", i, got[i], v.raw)
		}
	}
}

func TestSplitOneByteAtATime(t *testing.T) {
	var stream []byte
	for _, v := range pythonFrames {
		stream = append(stream, v.raw...)
	}

	got := scanAll(t, stream, true)
	if len(got) != len(pythonFrames) {
		t.Fatalf("got %d frames, want %d", len(got), len(pythonFrames))
	}
	for i, v := range pythonFrames {
		if !bytes.Equal(got[i], v.raw) {
			t.Errorf("frame %d = %q, want %q", i, got[i], v.raw)
		}
	}
}

func TestSplitKeepsCarriageReturnsInPayload(t *testing.T) {
	f := powerlink31.Frame{
		Type:    powerlink31.TypeBBA,
		MsgID:   1,
		Account: account,
		Panel:   panel,
		Data:    []byte{0x0d, 0xb0, 0x0d, 0x0d, 0x0d, 0x0a},
	}
	raw := f.Encode()

	got := scanAll(t, raw, false)
	if len(got) != 1 {
		t.Fatalf("got %d frames, want 1, so a payload 0x0d was read as a terminator", len(got))
	}
	if !bytes.Equal(got[0], raw) {
		t.Errorf("frame = %q, want %q", got[0], raw)
	}
}

func TestSplitResyncsPastRubbish(t *testing.T) {
	good := pythonFrames[1].raw

	stream := append([]byte("total rubbish with no frame in it"), good...)

	got := scanAll(t, stream, false)
	if len(got) != 1 {
		t.Fatalf("got %d frames, want 1", len(got))
	}
	if !bytes.Equal(got[0], good) {
		t.Errorf("frame = %q, want %q", got[0], good)
	}
}

func TestSplitDropsTruncatedTail(t *testing.T) {
	whole := pythonFrames[1].raw
	stream := append(bytes.Clone(whole), whole[:len(whole)-4]...)

	got := scanAll(t, stream, false)
	if len(got) != 1 {
		t.Fatalf("got %d frames, want 1 whole frame and the tail dropped", len(got))
	}
	if !bytes.Equal(got[0], whole) {
		t.Errorf("frame = %q, want %q", got[0], whole)
	}
}
