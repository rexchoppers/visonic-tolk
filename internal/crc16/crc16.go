// Package crc16 computes the CRC-16/ARC checksum every powerlink31 frame
// carries, so a receiver can tell a corrupted message from a good one.
//
// It catches accidents in transit, nothing more. It is not a signature and
// proves nothing about who sent the message.
package crc16

// Sum returns the checksum of data.
//
//	in:  123456789
//	out: 0xbb3d
//
// 0xa001 is 0x8005 bit reversed. The python this replaces reflects every input
// byte through a 256 entry table and shifts the other way to reach the same
// answer, so anyone diffing the two will find 0x8005 there. It is not a
// mistake in either direction, and the tests compare against vectors generated
// by that python rather than trusting the two forms are equivalent.
func Sum(data []byte) uint16 {
	var crc uint16
	for _, b := range data {
		crc ^= uint16(b)
		for range 8 {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xa001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}
