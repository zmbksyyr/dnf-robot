package crypt

import "encoding/binary"

// The anti NSEA variant builds its final S boxes from a length-prefixed key
// followed by zeros. The preliminary LCG permutations are overwritten before
// any block is processed. Each box starts with four copies of each byte value;
// no binary tables or original implementation are needed here.
func decryptAntiNSEA(key, data []byte) {
	var schedule [2048]byte
	schedule[1] = 16
	copy(schedule[2:18], key[:16])
	var boxes [2][1024]byte
	for box := range boxes {
		for i := range boxes[box] {
			boxes[box][i] = byte(i / 4)
		}
		for i := range boxes[box] {
			j := (i&3)*256 + int(schedule[box*1024+i])
			boxes[box][i], boxes[box][j] = boxes[box][j], boxes[box][i]
		}
	}
	var substitutions [2][256]uint32
	for box := range substitutions {
		for i := range substitutions[box] {
			substitutions[box][i] = binary.LittleEndian.Uint32(boxes[box][i*4:])
		}
	}
	var previous [4]uint32
	for offset := 0; offset+16 <= len(data); offset += 16 {
		part := data[offset : offset+16]
		var ciphertext [4]uint32
		for i := range ciphertext {
			ciphertext[i] = binary.LittleEndian.Uint32(part[i*4:])
		}
		a, b, c, d := ciphertext[0], ciphertext[1], ciphertext[2], ciphertext[3]
		for round := 0; round < 16; round++ {
			x, y := a>>8|b<<24, b>>8|a<<24
			a, b, c, d = c^substitutions[0][byte(x)], d^substitutions[1][byte(y)], x, y
		}
		for i, value := range [4]uint32{a, b, c, d} {
			binary.LittleEndian.PutUint32(part[i*4:], value^previous[i])
		}
		previous = ciphertext
	}
	n := len(data) / 16 * 16
	decryptAntiSeattos(key, data[n:])
}
