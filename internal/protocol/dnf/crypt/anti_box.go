package crypt

// BOX3D is a signed bit permutation. Its three working rings are derived from
// the original wire behavior, without the original linked-node implementation.
// See doc/review-20261121.md for the measurement and boundary conventions.
type antiBoxStep struct {
	bit    uint8
	up     uint8
	delta  int8
	invert uint8 // 0: neither ring, 1: vertical ring, 2: horizontal ring
}

// Each entry describes the encryption move for a zero key bit. A one bit
// reverses both rotations and transfers the inversion to the other ring.
var antiBoxSteps = [4][16]antiBoxStep{
	{
		{6, 0, 1, 1}, {3, 1, -1, 2}, {5, 0, 1, 0}, {4, 0, 1, 0},
		{5, 1, 1, 1}, {3, 0, -1, 0}, {4, 1, 1, 0}, {2, 1, 1, 1},
		{2, 0, -1, 2}, {6, 1, 1, 0}, {1, 0, 1, 1}, {0, 0, -1, 0},
		{0, 1, -1, 0}, {7, 0, -1, 2}, {7, 1, -1, 0}, {1, 1, -1, 2},
	},
	{
		{3, 0, -1, 0}, {2, 0, -1, 2}, {7, 0, 1, 1}, {7, 1, -1, 2},
		{4, 0, 1, 1}, {6, 1, 1, 0}, {1, 0, -1, 0}, {5, 1, 1, 1},
		{3, 1, -1, 0}, {6, 0, 1, 0}, {2, 1, 1, 1}, {4, 1, -1, 0},
		{5, 0, 1, 0}, {0, 0, -1, 2}, {1, 1, -1, 2}, {0, 1, -1, 0},
	},
	{
		{3, 0, -1, 0}, {2, 0, -1, 2}, {7, 0, 1, 0}, {4, 0, 1, 1},
		{6, 1, 1, 0}, {1, 0, -1, 0}, {5, 1, 1, 1}, {3, 1, -1, 0},
		{6, 0, 1, 0}, {2, 1, 1, 1}, {7, 1, -1, 2}, {4, 1, -1, 0},
		{5, 0, 1, 0}, {0, 0, -1, 2}, {1, 1, -1, 2}, {0, 1, -1, 0},
	},
	{
		{3, 0, -1, 0}, {2, 0, -1, 2}, {2, 1, 1, 1}, {7, 1, -1, 2},
		{4, 1, -1, 0}, {7, 0, 1, 0}, {4, 0, 1, 1}, {6, 1, 1, 0},
		{1, 0, -1, 0}, {5, 1, 1, 1}, {3, 1, -1, 0}, {6, 0, 1, 0},
		{5, 0, 1, 0}, {0, 0, -1, 2}, {1, 1, -1, 2}, {0, 1, -1, 0},
	},
}

func antiBoxBitOrder(length, index int) [8]uint8 {
	if length == 1 {
		return [8]uint8{2, 6, 5, 1, 3, 7, 4, 0}
	}
	if length == 2 {
		if index == 0 {
			return [8]uint8{7, 1, 5, 4, 3, 2, 6, 0}
		}
		return [8]uint8{7, 6, 2, 4, 3, 5, 1, 0}
	}
	if length == 3 {
		return [3][8]uint8{
			{7, 2, 5, 4, 3, 1, 6, 0},
			{7, 6, 4, 2, 3, 5, 1, 0},
			{3, 4, 2, 6, 7, 5, 1, 0},
		}[index]
	}
	switch index {
	case 0:
		return [8]uint8{7, 1, 4, 5, 0, 6, 2, 3}
	case 1, 5, 6:
		return [8]uint8{7, 5, 2, 0, 3, 6, 1, 4}
	case 2:
		return [8]uint8{3, 4, 0, 6, 7, 5, 1, 2}
	default:
		return [8]uint8{7, 4, 0, 6, 2, 5, 3, 1}
	}
}

// The legacy BOX3D handler transforms only length%8 bytes. Its anti callers
// (IDEA, DesNew, DesBig, CAST and Tean) all use eight-byte blocks. Square and MARS use
// Seattos instead, including the entire residual slice of up to fifteen bytes.
func decryptAntiBoxTail(key, data []byte) {
	data = data[len(data)/8*8:]
	n := len(data)
	if n == 0 {
		return
	}
	var values [56]byte
	for i, value := range data {
		for j, bit := range antiBoxBitOrder(n, i) {
			values[i*8+j] = (value>>bit)&1 ^ byte(j&1)
		}
	}
	var vertical [28]uint8
	copy(vertical[:], []uint8{0, 4, 6, 2})
	count := 4
	if n >= 2 {
		copy(vertical[:], []uint8{0, 4, 8, 12, 14, 10, 6, 2})
		count = 8
	}
	for layer := 2; layer < n; layer++ {
		anchor, base := uint8(8*(layer-2)), uint8(8*layer)
		for i := 0; i < count; i++ {
			if vertical[i] == anchor {
				copy(vertical[i+5:], vertical[i+1:count])
				copy(vertical[i+1:], []uint8{base + 2, base, base + 4, base + 6})
				count += 4
				break
			}
		}
	}
	horizontal := []uint8{0, 4, 8, 12, 13, 9, 5, 1}
	if n == 1 {
		horizontal = []uint8{0, 4, 5, 1}
	}
	variant := min(n-1, 3)
	for k := len(key) - 1; k >= 0; k-- {
		for s := 15; s >= 0; s-- {
			step := antiBoxSteps[variant][s]
			delta, invert := int(step.delta), step.invert
			if key[k]&(1<<step.bit) != 0 {
				delta = -delta
				if invert != 0 {
					invert = 3 - invert
				}
			}
			var flipUp, flipLeft byte
			if invert == 1 {
				flipUp = 1
			} else if invert == 2 {
				flipLeft = 1
			}
			// Reverse the pair's order as well as both rotation directions.
			rotateAntiBoxRing(&values, horizontal, 0, -delta, flipLeft)
			rotateAntiBoxRing(&values, vertical[:count], step.up, -delta, flipUp)
		}
	}
	for i := range data {
		var value byte
		for j, bit := range antiBoxBitOrder(n, i) {
			value |= (values[i*8+j] ^ byte(j&1)) << bit
		}
		data[i] = value
	}
}

func rotateAntiBoxRing(values *[56]byte, ring []uint8, offset uint8, delta int, flip byte) {
	if delta > 0 {
		first := values[ring[0]+offset]
		for i := 0; i+1 < len(ring); i++ {
			values[ring[i]+offset] = values[ring[i+1]+offset] ^ flip
		}
		values[ring[len(ring)-1]+offset] = first ^ flip
	} else {
		last := values[ring[len(ring)-1]+offset]
		for i := len(ring) - 1; i > 0; i-- {
			values[ring[i]+offset] = values[ring[i-1]+offset] ^ flip
		}
		values[ring[0]+offset] = last ^ flip
	}
}
