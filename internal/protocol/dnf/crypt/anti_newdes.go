package crypt

// The NewDES substitution is the published algorithm constant, released into
// the public domain by Robert Scott with his March 2, 1996 NewDES revision.
// See doc/review-20261121.md for the source and wire-variant measurements.
var antiNewDESSBox = [256]byte{
	32, 137, 239, 188, 102, 125, 221, 72, 212, 68, 81, 37, 86, 237, 147, 149,
	70, 229, 17, 124, 115, 207, 33, 20, 122, 143, 25, 215, 51, 183, 138, 142,
	146, 211, 110, 173, 1, 228, 189, 14, 103, 78, 162, 36, 253, 167, 116, 255,
	158, 45, 185, 50, 98, 168, 250, 235, 54, 141, 195, 247, 240, 63, 148, 2,
	224, 169, 214, 180, 62, 22, 117, 108, 19, 172, 161, 159, 160, 47, 43, 171,
	194, 175, 178, 56, 196, 112, 23, 220, 89, 21, 164, 130, 157, 8, 85, 251,
	216, 44, 94, 179, 226, 38, 90, 119, 40, 202, 34, 206, 35, 69, 231, 246,
	29, 109, 74, 71, 176, 6, 60, 145, 65, 13, 77, 151, 12, 127, 95, 199,
	57, 101, 5, 232, 150, 210, 129, 24, 181, 10, 121, 187, 48, 193, 139, 252,
	219, 64, 88, 233, 96, 128, 80, 53, 191, 144, 218, 11, 106, 132, 155, 104,
	91, 136, 31, 42, 243, 66, 126, 135, 30, 26, 87, 186, 182, 154, 242, 123,
	82, 166, 208, 39, 152, 190, 113, 205, 114, 105, 225, 84, 73, 163, 99, 111,
	204, 61, 200, 217, 170, 15, 198, 28, 192, 254, 134, 234, 222, 7, 236, 248,
	201, 41, 177, 156, 92, 131, 67, 249, 245, 184, 203, 9, 241, 0, 27, 46,
	133, 174, 75, 18, 93, 209, 100, 120, 76, 213, 16, 83, 4, 107, 140, 52,
	58, 55, 3, 244, 97, 197, 238, 227, 118, 49, 79, 230, 223, 165, 153, 59,
}

func decryptAntiNewDES(key, data []byte) {
	var schedule [60]byte
	for i := range schedule {
		mask := byte(0)
		if i >= 15 {
			mask = key[6+i/15]
		}
		schedule[i] = key[i%15] ^ mask
	}
	for offset := 0; offset+8 <= len(data); offset += 8 {
		block := data[offset : offset+8]
		for round := 8; round >= 0; round-- {
			for i := 0; i < 4; i++ {
				block[4+i] ^= antiNewDESSBox[block[i]^schedule[round*7+i]]
			}
			if round > 0 {
				i := round*7 - 3
				block[0] ^= antiNewDESSBox[block[7]^schedule[i+2]]
				block[3] ^= antiNewDESSBox[block[6]^schedule[i+1]]
				block[2] ^= antiNewDESSBox[block[4]^block[5]]
				block[1] ^= antiNewDESSBox[block[4]^schedule[i]]
			}
		}
	}
}
