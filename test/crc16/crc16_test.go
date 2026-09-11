package crc16test

import (
	"testing"

	"github.com/rexchoppers/visonic-tolk/internal/crc16"
)

func TestSumMatchesPython(t *testing.T) {
	for _, v := range pythonVectors {
		if got := crc16.Sum(v.in); got != v.out {
			t.Errorf("Sum(%x) = %#04x, python gives %#04x", v.in, got, v.out)
		}
	}
}

func TestSumCheckValue(t *testing.T) {
	if got := crc16.Sum([]byte("123456789")); got != 0xbb3d {
		t.Errorf("CRC-16/ARC check value = %#04x, want 0xbb3d", got)
	}
}
