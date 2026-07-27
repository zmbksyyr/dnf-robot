package crypt

type NoekeonCipher struct {
	K  [4]uint32
	dK [4]uint32
}

var noekeonRC = [17]uint32{
	0x00000080, 0x0000001b, 0x00000036, 0x0000006c,
	0x000000d8, 0x000000ab, 0x0000004d, 0x0000009a,
	0x0000002f, 0x0000005e, 0x000000bc, 0x00000063,
	0x000000c6, 0x00000097, 0x00000035, 0x0000006a,
	0x000000d4,
}

func NewNoekeonCipher() *NoekeonCipher {
	return &NoekeonCipher{}
}

func (n *NoekeonCipher) SetKey(key []byte) error {
	if len(key) != 16 {
		return ErrInvalidKeySize
	}
	n.K[0] = loadU32BE(key[0:4])
	n.K[1] = loadU32BE(key[4:8])
	n.K[2] = loadU32BE(key[8:12])
	n.K[3] = loadU32BE(key[12:16])
	n.dK[0] = n.K[0]
	n.dK[1] = n.K[1]
	n.dK[2] = n.K[2]
	n.dK[3] = n.K[3]
	kTheta(&n.dK[0], &n.dK[1], &n.dK[2], &n.dK[3])
	return nil
}

func (n *NoekeonCipher) KeySize() int {
	return 16
}

func (n *NoekeonCipher) BlockSize() int {
	return 16
}

func kTheta(a, b, c, d *uint32) {
	temp := *a ^ *c
	temp = temp ^ rol32(temp, 8) ^ ror32(temp, 8)
	*b ^= temp
	*d ^= temp
	temp = *b ^ *d
	temp = temp ^ rol32(temp, 8) ^ ror32(temp, 8)
	*a ^= temp
	*c ^= temp
}

func theta(k *[4]uint32, a, b, c, d *uint32) {
	temp := *a ^ *c
	temp = temp ^ rol32(temp, 8) ^ ror32(temp, 8)
	*b ^= temp ^ k[1]
	*d ^= temp ^ k[3]
	temp = *b ^ *d
	temp = temp ^ rol32(temp, 8) ^ ror32(temp, 8)
	*a ^= temp ^ k[0]
	*c ^= temp ^ k[2]
}

func gamma(a, b, c, d *uint32) {
	*b ^= ^(*d | *c)
	*a ^= *c & *b
	temp := *d
	*d = *a
	*a = temp
	*c ^= *a ^ *b ^ *d
	*b ^= ^(*d | *c)
	*a ^= *c & *b
}

func pi1(a, b, c, d *uint32) {
	*b = rol32(*b, 1)
	*c = rol32(*c, 5)
	*d = rol32(*d, 2)
}

func pi2(a, b, c, d *uint32) {
	*b = ror32(*b, 1)
	*c = ror32(*c, 5)
	*d = ror32(*d, 2)
}

func (n *NoekeonCipher) Encrypt(data []byte) ([]byte, error) {
	blockSize := n.BlockSize()
	if len(data)%blockSize != 0 {
		return nil, ErrInvalidBlockSize
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += blockSize {
		a := loadU32BE(data[i : i+4])
		b := loadU32BE(data[i+4 : i+8])
		c := loadU32BE(data[i+8 : i+12])
		d := loadU32BE(data[i+12 : i+16])
		for r := 0; r < 16; r++ {
			a ^= noekeonRC[r]
			theta(&n.K, &a, &b, &c, &d)
			pi1(&b, &a, &c, &d)
			gamma(&a, &b, &c, &d)
			pi2(&b, &a, &c, &d)
		}
		a ^= noekeonRC[16]
		theta(&n.K, &a, &b, &c, &d)
		storeU32BE(out[i:i+4], a)
		storeU32BE(out[i+4:i+8], b)
		storeU32BE(out[i+8:i+12], c)
		storeU32BE(out[i+12:i+16], d)
	}
	return out, nil
}

func (n *NoekeonCipher) Decrypt(data []byte) ([]byte, error) {
	blockSize := n.BlockSize()
	if len(data)%blockSize != 0 {
		return nil, ErrInvalidBlockSize
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += blockSize {
		a := loadU32BE(data[i : i+4])
		b := loadU32BE(data[i+4 : i+8])
		c := loadU32BE(data[i+8 : i+12])
		d := loadU32BE(data[i+12 : i+16])
		for r := 16; r > 0; r-- {
			theta(&n.dK, &a, &b, &c, &d)
			a ^= noekeonRC[r]
			pi1(&b, &a, &c, &d)
			gamma(&a, &b, &c, &d)
			pi2(&b, &a, &c, &d)
		}
		theta(&n.dK, &a, &b, &c, &d)
		a ^= noekeonRC[0]
		storeU32BE(out[i:i+4], a)
		storeU32BE(out[i+4:i+8], b)
		storeU32BE(out[i+8:i+12], c)
		storeU32BE(out[i+12:i+16], d)
	}
	return out, nil
}
