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

	Disconnect   = []byte{0x0d, 0xad, 0x0a, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x43, 0x05, 0x0a}
	Download     = []byte{0x0d, 0x09, 0xf6, 0x0a}
	ExitDownload = []byte{0x0d, 0x0f, 0xf0, 0x0a}
)

// Status is the e0 message tolk invents to tell Home Assistant what it can
// see. Home Assistant asks for it with an e1 01 action.
//
//	out: 0d e0 01 01 01 01 00 00 43 <checksum> 0a
func Status(panels, visonic, monitors int, proxy, stealth, download bool) []byte {
	return Wrap([]byte{
		0xe0,
		byte(panels), byte(visonic), byte(monitors),
		flag(proxy), flag(stealth), flag(download),
		0x43,
	})
}

func flag(on bool) byte {
	if on {
		return 0x01
	}
	return 0x00
}

// The message classes anything here branches on.
const (
	ClassDownload = 0x24 // home assistant reading the panel's eprom
	ClassPanel    = 0xb0 // panel state
	ClassStatus   = 0xe0 // what tolk can see, invented here
	ClassAction   = 0xe1 // home assistant asking tolk to do something
)

// Class is the byte that says what kind of message this is, b0 and e0 and e1
// being the ones that get looked at. Zero when the message is too short.
func Class(msg []byte) byte {
	if len(msg) < 2 {
		return 0
	}
	return msg[1]
}

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

// maxLen bounds the search for a terminator, so a corrupt stream resyncs
// rather than buffering without end.
const maxLen = 512

// Split cuts a stream of bare panel messages, for bufio.Scanner.
//
//	in:  0d e1 02 01 43 07 0a 0d e1 01 00 43 09 0a
//	out: the first message, leaving the second buffered
//
// There is no length to read, and 0x0a occurs inside bodies, so the end is
// the first terminator whose checksum adds up. The python reads one message
// per read instead, and loses the second whenever two arrive together.
func Split(data []byte, atEOF bool) (int, []byte, error) {
	if len(data) == 0 {
		return 0, nil, nil
	}

	// A bare body carries no framing, so there is no boundary to find and the
	// read has to be taken as it came. Home Assistant may send one of these.
	if data[0] != 0x0d {
		return len(data), data, nil
	}

	for i := 2; i < len(data) && i <= maxLen; i++ {
		if data[i] != 0x0a {
			continue
		}
		if Checksum(data[1:i-1]) == data[i-1] {
			return i + 1, data[:i+1], nil
		}
	}

	// Nothing adds up within a message's worth of bytes, so this 0x0d did not
	// start one. Step over it rather than buffering for ever.
	if len(data) > maxLen {
		return 1, nil, nil
	}

	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
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
