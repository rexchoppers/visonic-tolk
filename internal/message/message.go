// Package message handles the panel message that sits inside a powerlink31
// frame, and that Home Assistant exchanges directly on the monitor port.
//
// A message runs 0x0d, a body, a checksum byte, then 0x0a.
package message

import (
	"bytes"
	"errors"
	"slices"
)

var (
	PLAck     = []byte{0x0d, 0x02, 0x43, 0xba, 0x0a}
	Ack       = []byte{0x0d, 0x02, 0xfd, 0x0a}
	Keepalive = []byte{0x0d, 0xb0, 0x01, 0x6a, 0x00, 0x43, 0xa0, 0x0a}
)

var (
	ErrEmpty     = errors.New("empty message")
	ErrShorthand = errors.New("b0 shorthand request is not supported")
)

// Checksum returns the trailing check byte for a message body.
//
//	in:  b0 01 6a 00 43
//	out: 0xa0
//
// The modulus is 0xff, not 0x100, and a result of 0xff becomes 0x00. Both are
// deliberate and both come from the python being replaced.
func Checksum(body []byte) byte {
	var sum int
	for _, b := range body {
		sum += int(b)
	}

	check := 0xff - (sum % 0xff)
	if check == 0xff {
		return 0x00
	}
	return byte(check)
}

// Wrap turns a bare body into a whole message.
//
//	in:  b0 01 6a 00 43
//	out: 0d b0 01 6a 00 43 a0 0a
func Wrap(body []byte) []byte {
	out := make([]byte, 0, len(body)+3)
	out = append(out, 0x0d)
	out = append(out, body...)
	out = append(out, Checksum(body))
	out = append(out, 0x0a)
	return out
}

// IsAck reports whether a whole message is an acknowledgement.
func IsAck(msg []byte) bool {
	return len(msg) > 1 && msg[0] == 0x0d && msg[1] == 0x02
}

// FromMonitor takes what Home Assistant sent and returns the message to put
// inside a frame, and whether it is an acknowledgement.
//
//	in:  0d b0 01 6a 00 43 a0 0a   out: unchanged
//	in:  0d 02 fd 0a               out: 0d 02 43 ba 0a, the powerlink ack
//	in:  a2 00 00 08 00 00 43      out: wrapped as a message
//
// A b0 body that does not end in 0x43 is shorthand the python expands through
// per command builders. Nothing has been seen using it, so it is refused
// loudly here rather than expanded wrongly in silence.
func FromMonitor(b []byte) ([]byte, bool, error) {
	if len(b) == 0 {
		return nil, false, ErrEmpty
	}

	if b[0] == 0x0d && b[len(b)-1] == 0x0a {
		if !IsAck(b) {
			return b, false, nil
		}
		if bytes.Equal(b, Ack) {
			return slices.Clone(PLAck), true, nil
		}
		return b, true, nil
	}

	if b[0] == 0xb0 && b[len(b)-1] != 0x43 {
		return nil, false, ErrShorthand
	}

	return Wrap(b), false, nil
}
