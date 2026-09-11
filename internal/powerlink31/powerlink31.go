// Package powerlink31 encodes and decodes the frame format Visonic panels
// speak. Every message on the wire is wrapped in one.
//
// A frame, with the payload shown as hex:
//
//	\n 4CE3 0023 "VIS-BBA" 0001 L001234 #2A4CC3 [ 0d b0 03 18 0d 0a ] \r
//	   |    |    |         |    |       |       |                      |
//	   |    |    |         |    |       |       payload                terminator
//	   |    |    |         |    |       panel id
//	   |    |    |         |    account id
//	   |    |    |         message id, four digit decimal
//	   |    |    message type
//	   |    length of the first quote through the closing bracket
//	   checksum of that same range
package powerlink31

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/rexchoppers/visonic-tolk/internal/crc16"
)

const (
	TypeBBA    = "VIS-BBA"
	TypeAck    = "VIS-ACK"
	TypeAdmCID = "*ADM-CID"
	TypeAdmAck = "*ACK"
	TypeNak    = "NAK"
)

var ErrMalformed = errors.New("malformed powerlink31 frame")

// Frame is one decoded message.
type Frame struct {
	Type    string // one of the Type constants above
	MsgID   int    // counter, echoed back by the ACK that answers this frame
	Account string
	Panel   string
	Data    []byte // payload between the brackets
	Raw     []byte // exactly as it arrived, for forwarding untouched
}

// Decode reads one frame off the wire.
//
//	in:  \n4CE30023"VIS-BBA"0001L001234#2A4CC3[\r\xb0\x03\x18\r\n]\r
//	out: Frame{Type: "VIS-BBA", MsgID: 1, Account: "001234", Panel: "2A4CC3", Data: 0d b0 03 18 0d 0a}
//
// The payload is sliced three different ways depending on type. A normal frame
// drops the closing bracket and the terminator. *ADM-CID and *ACK carry no
// closing bracket. NAK skips an empty bracket pair and an underscore, leaving
// only a timestamp.
func Decode(b []byte) (Frame, error) {
	if len(b) < 12 || b[0] != '\n' || b[len(b)-1] != '\r' {
		return Frame{}, ErrMalformed
	}

	open := bytes.IndexByte(b, '[')
	if open < 9 {
		return Frame{}, ErrMalformed
	}

	head := string(b[1:open])
	typ, err := frameType(head)
	if err != nil {
		return Frame{}, err
	}

	f := Frame{Type: typ, Raw: b}

	switch typ {
	case TypeNak:
		if open+3 > len(b)-1 {
			return Frame{}, ErrMalformed
		}
		f.MsgID, f.Account, f.Panel = 0, "0", "0"
		f.Data = b[open+3 : len(b)-1]
	case TypeAdmCID, TypeAdmAck:
		if err := address(head, &f); err != nil {
			return Frame{}, err
		}
		f.Data = b[open+1 : len(b)-1]
	default:
		if err := address(head, &f); err != nil {
			return Frame{}, err
		}
		if open+1 > len(b)-2 {
			return Frame{}, ErrMalformed
		}
		f.Data = b[open+1 : len(b)-2]
	}

	return f, nil
}

// Encode builds the bytes to put on the wire.
//
//	in:  Frame{Type: "VIS-BBA", MsgID: 1, Account: "001234", Panel: "2A4CC3", Data: 0d b0 03 18 0d 0a}
//	out: \n4CE30023"VIS-BBA"0001L001234#2A4CC3[\r\xb0\x03\x18\r\n]\r
//
// Only VIS-BBA and VIS-ACK are ever built. Every other type is forwarded as it
// arrived, so nothing rebuilds one.
func (f Frame) Encode() []byte {
	base := make([]byte, 0, len(f.Data)+32)
	base = append(base, '"')
	base = append(base, f.Type...)
	base = append(base, '"')
	base = append(base, fmt.Sprintf("%04d", f.MsgID)...)
	base = append(base, 'L')
	base = append(base, f.Account...)
	base = append(base, '#')
	base = append(base, f.Panel...)
	base = append(base, '[')
	base = append(base, f.Data...)
	base = append(base, ']')

	out := make([]byte, 0, len(base)+10)
	out = append(out, '\n')
	// The checksum goes out uppercase and the length lowercase. Matching the python exactly.
	out = append(out, fmt.Sprintf("%04X%04x", crc16.Sum(base), len(base))...)
	out = append(out, base...)
	out = append(out, '\r')
	return out
}

// frameType pulls the quoted message type out of the header.
//
//	in:  4CE30023"VIS-BBA"0001L001234#2A4CC3
//	out: VIS-BBA
func frameType(head string) (string, error) {
	open := strings.IndexByte(head, '"')
	if open < 0 {
		return "", ErrMalformed
	}

	end := strings.IndexByte(head[open+1:], '"')
	if end < 0 {
		return "", ErrMalformed
	}

	return head[open+1 : open+1+end], nil
}

// address fills in who the frame is from and which message it is.
//
//	in:  4CE30023"VIS-BBA"0001L001234#2A4CC3
//	out: MsgID 1, Account 001234, Panel 2A4CC3
//
// None of the three is delimited. The message id is the four characters before
// the L, the account runs from the L to the #, and the panel is the six after
// the #. NAK frames carry none of it and never reach here.
func address(head string, f *Frame) error {
	l := strings.IndexByte(head, 'L')
	hash := strings.IndexByte(head, '#')
	if l < 4 || hash < l || hash+7 > len(head) {
		return ErrMalformed
	}

	id, err := strconv.Atoi(head[l-4 : l])
	if err != nil {
		return ErrMalformed
	}

	f.MsgID = id
	f.Account = head[l+1 : hash]
	f.Panel = head[hash+1 : hash+7]
	return nil
}
