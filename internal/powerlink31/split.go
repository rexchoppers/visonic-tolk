package powerlink31

import (
	"bytes"
	"strconv"
)

// headerLen is the newline, four checksum characters and four length
// characters that precede every frame body.
const headerLen = 9

// SplitFrames cuts a TCP stream into whole frames, for bufio.Scanner.
//
//	in:  \n4CE30023"VIS-BBA"0001L001234#2A4CC3[..]\r\n0A12005f"VIS-BB
//	out: the first frame, leaving the partial second one buffered
//
// The end of a frame is found from the length it declares, never by scanning
// for a terminator. 0x0d is both the frame terminator and a common payload
// byte, so scanning would truncate a frame at the first payload byte that
// happened to be 0x0d.
//
// A frame that does not end where its length says is skipped, and the scan
// resumes at the next newline, so one corrupt frame cannot swallow the stream.
func SplitFrames(data []byte, atEOF bool) (int, []byte, error) {
	start := bytes.IndexByte(data, '\n')
	if start < 0 {
		return drain(data, atEOF)
	}

	rest := data[start:]
	if len(rest) < headerLen {
		return drain(data, atEOF)
	}

	body, err := strconv.ParseUint(string(rest[5:headerLen]), 16, 32)
	if err != nil {
		return start + 1, nil, nil
	}

	total := headerLen + int(body) + 1
	if len(rest) < total {
		return drain(data, atEOF)
	}

	if rest[total-1] != '\r' {
		return start + 1, nil, nil
	}

	return start + total, rest[:total], nil
}

func drain(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF {
		return len(data), nil, nil
	}
	return 0, nil, nil
}
