package crypt

import (
	"encoding/binary"
	"math/bits"
)

// Fixed protocol constants recovered from chosen-key SetKey input/output
// measurements. The product does not load or call the original library.
var antiMMBScheduleTable = [64]uint32{
	0xfa5d7cec, 0x734533f8, 0x484ea4b2, 0x2bf0e46b,
	0xa853884b, 0xcbe7dee0, 0x976df92a, 0xe2b17575,
	0xf9bc1efd, 0xf8adc573, 0x2e5b6ea9, 0x1c5a4ffd,
	0x3df27762, 0x2d7bf0bd, 0x039d26d6, 0x0a937d09,
	0x01052bc2, 0x8aeeea1c, 0xfb7d3ebe, 0x2449a182,
	0x17b71ba3, 0xc6e68955, 0x74c7a0cd, 0xe1b88745,
	0x4d71958c, 0x149ceb88, 0x5344e0b9, 0xc3f0a21f,
	0xa348429b, 0xf672c8cd, 0xd01fa55c, 0x4c6fd3df,
	0xc05b564e, 0xe0d81861, 0xa3b3eaac, 0x17ae4a08,
	0x9d8df3bf, 0x1353cbfe, 0x25c0f1c8, 0xd52818ec,
	0x1ba102b0, 0xc2fdc394, 0x9ab0505e, 0xf8d5f8cb,
	0x708749da, 0x34c5d60b, 0xd1c046b0, 0x4c8e4672,
	0x0e236a84, 0x5a36c5ab, 0xdc9b9eb5, 0x0adb5718,
	0x57d27cc5, 0xc4ecaefd, 0xb57c0d9e, 0x764e92ea,
	0x4ca01ed6, 0xa7314405, 0xee5e6b4b, 0xf9560189,
	0xc50cd4d5, 0xbfe0afd8, 0x8322f984, 0x65601b60,
}

func decryptAntiMMB(key, data []byte) {
	a := binary.LittleEndian.Uint32(key) - 0x33cfc738
	b := binary.LittleEndian.Uint32(key[4:]) + bits.RotateLeft32(a, 5)
	c := binary.LittleEndian.Uint32(key[8:]) + bits.RotateLeft32(b, 7)
	d := binary.LittleEndian.Uint32(key[12:]) + bits.RotateLeft32(c, 11)
	selector := bits.RotateLeft32(d, 1) & 63
	mixed := (bits.RotateLeft32(d, 2) + antiMMBScheduleTable[selector]) ^ 0x19860719
	x := bits.RotateLeft32(mixed, 1) ^ a
	y := bits.RotateLeft32(x, 2) ^ b
	z := bits.RotateLeft32(y, 3) ^ c
	schedule := [4]uint32{x, x ^ y, x ^ y ^ z, x ^ y ^ z ^ d ^ bits.RotateLeft32(z, 4)}

	for offset := 0; offset+16 <= len(data); offset += 16 {
		var words [4]uint32
		for i := range words {
			words[i] = binary.LittleEndian.Uint32(data[offset+i*4:])
		}
		// Six inverse rounds, with a rotating key XOR at both boundaries.
		for round := 6; round >= 0; round-- {
			for i := range words {
				words[i] ^= schedule[(i+round)&3]
			}
			if round > 0 {
				a, b, c, d := words[0], words[1], words[2], words[3]
				words = [4]uint32{a ^ b ^ d, b ^ a ^ c, c ^ b ^ d, d ^ a ^ c}
				if words[0]&1 != 0 {
					words[0] ^= 0x2aaaaaaa
				}
				for i, inverse := range [...]uint32{0x0dad4694, 0x06d6a34a, 0x81b5a8d2, 0x281b5a8d} {
					words[i] = antiMMBMultiply(words[i], inverse)
				}
			}
		}
		for i, word := range words {
			binary.LittleEndian.PutUint32(data[offset+i*4:], word)
		}
	}
}

// Multiplication modulo 2^32-1 with end-around carries. Preserve the all-one
// representation of zero, which the wire variant distinguishes from all-zero.
func antiMMBMultiply(a, b uint32) uint32 {
	product := uint64(a) * uint64(b)
	sum := uint64(uint32(product)) + product>>32
	return uint32(sum) + uint32(sum>>32)
}
