package crypt

type Cast5Cipher struct {
	K      [32]uint32
	keylen int
}

func NewCast5Cipher() *Cast5Cipher {
	return &Cast5Cipher{}
}

func (c *Cast5Cipher) SetKey(key []byte) error {
	if len(key) < 5 || len(key) > 16 {
		return ErrInvalidKeySize
	}
	cast5Setup(c, key, len(key))
	return nil
}

func (c *Cast5Cipher) KeySize() int {
	return 16
}

func (c *Cast5Cipher) BlockSize() int {
	return 8
}

func gb(x [4]uint32, i int) uint32 {
	return (x[(15-i)>>2] >> uint(8*((15-i)&3))) & 255
}

func cast5FI(R, Km, Kr uint32) uint32 {
	I := Km + R
	I = rol32(I, int(Kr))
	return (S1[byte(I>>24)] ^ S2[byte(I>>16)]) - S3[byte(I>>8)] + S4[byte(I)]
}

func cast5FII(R, Km, Kr uint32) uint32 {
	I := Km ^ R
	I = rol32(I, int(Kr))
	return (S1[byte(I>>24)] - S2[byte(I>>16)]) + S3[byte(I>>8)] ^ S4[byte(I)]
}

func cast5FIII(R, Km, Kr uint32) uint32 {
	I := Km - R
	I = rol32(I, int(Kr))
	return (S1[byte(I>>24)] + S2[byte(I>>16)]) ^ S3[byte(I>>8)] - S4[byte(I)]
}

func cast5Setup(c *Cast5Cipher, key []byte, keylen int) {
	var x, z [4]uint32
	buf := make([]byte, 16)
	copy(buf, key)
	for y := 0; y < 4; y++ {
		x[3-y] = loadU32BE(buf[4*y : 4*y+4])
	}
	i := 0
	for y := 0; y < 2; y++ {
		z[3] = x[3] ^ S5[gb(x, 0xD)] ^ S6[gb(x, 0xF)] ^ S7[gb(x, 0xC)] ^ S8[gb(x, 0xE)] ^ S7[gb(x, 0x8)]
		z[2] = x[1] ^ S5[gb(z, 0x0)] ^ S6[gb(z, 0x2)] ^ S7[gb(z, 0x1)] ^ S8[gb(z, 0x3)] ^ S8[gb(x, 0xA)]
		z[1] = x[0] ^ S5[gb(z, 0x7)] ^ S6[gb(z, 0x6)] ^ S7[gb(z, 0x5)] ^ S8[gb(z, 0x4)] ^ S5[gb(x, 0x9)]
		z[0] = x[2] ^ S5[gb(z, 0xA)] ^ S6[gb(z, 0x9)] ^ S7[gb(z, 0xb)] ^ S8[gb(z, 0x8)] ^ S6[gb(x, 0xB)]
		c.K[i] = S5[gb(z, 0x8)] ^ S6[gb(z, 0x9)] ^ S7[gb(z, 0x7)] ^ S8[gb(z, 0x6)] ^ S5[gb(z, 0x2)]
		i++
		c.K[i] = S5[gb(z, 0xA)] ^ S6[gb(z, 0xB)] ^ S7[gb(z, 0x5)] ^ S8[gb(z, 0x4)] ^ S6[gb(z, 0x6)]
		i++
		c.K[i] = S5[gb(z, 0xC)] ^ S6[gb(z, 0xd)] ^ S7[gb(z, 0x3)] ^ S8[gb(z, 0x2)] ^ S7[gb(z, 0x9)]
		i++
		c.K[i] = S5[gb(z, 0xE)] ^ S6[gb(z, 0xF)] ^ S7[gb(z, 0x1)] ^ S8[gb(z, 0x0)] ^ S8[gb(z, 0xc)]
		i++

		x[3] = z[1] ^ S5[gb(z, 0x5)] ^ S6[gb(z, 0x7)] ^ S7[gb(z, 0x4)] ^ S8[gb(z, 0x6)] ^ S7[gb(z, 0x0)]
		x[2] = z[3] ^ S5[gb(x, 0x0)] ^ S6[gb(x, 0x2)] ^ S7[gb(x, 0x1)] ^ S8[gb(x, 0x3)] ^ S8[gb(z, 0x2)]
		x[1] = z[2] ^ S5[gb(x, 0x7)] ^ S6[gb(x, 0x6)] ^ S7[gb(x, 0x5)] ^ S8[gb(x, 0x4)] ^ S5[gb(z, 0x1)]
		x[0] = z[0] ^ S5[gb(x, 0xA)] ^ S6[gb(x, 0x9)] ^ S7[gb(x, 0xb)] ^ S8[gb(x, 0x8)] ^ S6[gb(z, 0x3)]
		c.K[i] = S5[gb(x, 0x3)] ^ S6[gb(x, 0x2)] ^ S7[gb(x, 0xc)] ^ S8[gb(x, 0xd)] ^ S5[gb(x, 0x8)]
		i++
		c.K[i] = S5[gb(x, 0x1)] ^ S6[gb(x, 0x0)] ^ S7[gb(x, 0xe)] ^ S8[gb(x, 0xf)] ^ S6[gb(x, 0xd)]
		i++
		c.K[i] = S5[gb(x, 0x7)] ^ S6[gb(x, 0x6)] ^ S7[gb(x, 0x8)] ^ S8[gb(x, 0x9)] ^ S7[gb(x, 0x3)]
		i++
		c.K[i] = S5[gb(x, 0x5)] ^ S6[gb(x, 0x4)] ^ S7[gb(x, 0xa)] ^ S8[gb(x, 0xb)] ^ S8[gb(x, 0x7)]
		i++

		z[3] = x[3] ^ S5[gb(x, 0xD)] ^ S6[gb(x, 0xF)] ^ S7[gb(x, 0xC)] ^ S8[gb(x, 0xE)] ^ S7[gb(x, 0x8)]
		z[2] = x[1] ^ S5[gb(z, 0x0)] ^ S6[gb(z, 0x2)] ^ S7[gb(z, 0x1)] ^ S8[gb(z, 0x3)] ^ S8[gb(x, 0xA)]
		z[1] = x[0] ^ S5[gb(z, 0x7)] ^ S6[gb(z, 0x6)] ^ S7[gb(z, 0x5)] ^ S8[gb(z, 0x4)] ^ S5[gb(x, 0x9)]
		z[0] = x[2] ^ S5[gb(z, 0xA)] ^ S6[gb(z, 0x9)] ^ S7[gb(z, 0xb)] ^ S8[gb(z, 0x8)] ^ S6[gb(x, 0xB)]
		c.K[i] = S5[gb(z, 0x3)] ^ S6[gb(z, 0x2)] ^ S7[gb(z, 0xc)] ^ S8[gb(z, 0xd)] ^ S5[gb(z, 0x9)]
		i++
		c.K[i] = S5[gb(z, 0x1)] ^ S6[gb(z, 0x0)] ^ S7[gb(z, 0xe)] ^ S8[gb(z, 0xf)] ^ S6[gb(z, 0xc)]
		i++
		c.K[i] = S5[gb(z, 0x7)] ^ S6[gb(z, 0x6)] ^ S7[gb(z, 0x8)] ^ S8[gb(z, 0x9)] ^ S7[gb(z, 0x2)]
		i++
		c.K[i] = S5[gb(z, 0x5)] ^ S6[gb(z, 0x4)] ^ S7[gb(z, 0xa)] ^ S8[gb(z, 0xb)] ^ S8[gb(z, 0x6)]
		i++

		x[3] = z[1] ^ S5[gb(z, 0x5)] ^ S6[gb(z, 0x7)] ^ S7[gb(z, 0x4)] ^ S8[gb(z, 0x6)] ^ S7[gb(z, 0x0)]
		x[2] = z[3] ^ S5[gb(x, 0x0)] ^ S6[gb(x, 0x2)] ^ S7[gb(x, 0x1)] ^ S8[gb(x, 0x3)] ^ S8[gb(z, 0x2)]
		x[1] = z[2] ^ S5[gb(x, 0x7)] ^ S6[gb(x, 0x6)] ^ S7[gb(x, 0x5)] ^ S8[gb(x, 0x4)] ^ S5[gb(z, 0x1)]
		x[0] = z[0] ^ S5[gb(x, 0xA)] ^ S6[gb(x, 0x9)] ^ S7[gb(x, 0xb)] ^ S8[gb(x, 0x8)] ^ S6[gb(z, 0x3)]
		c.K[i] = S5[gb(x, 0x8)] ^ S6[gb(x, 0x9)] ^ S7[gb(x, 0x7)] ^ S8[gb(x, 0x6)] ^ S5[gb(x, 0x3)]
		i++
		c.K[i] = S5[gb(x, 0xa)] ^ S6[gb(x, 0xb)] ^ S7[gb(x, 0x5)] ^ S8[gb(x, 0x4)] ^ S6[gb(x, 0x7)]
		i++
		c.K[i] = S5[gb(x, 0xc)] ^ S6[gb(x, 0xd)] ^ S7[gb(x, 0x3)] ^ S8[gb(x, 0x2)] ^ S7[gb(x, 0x8)]
		i++
		c.K[i] = S5[gb(x, 0xe)] ^ S6[gb(x, 0xf)] ^ S7[gb(x, 0x1)] ^ S8[gb(x, 0x0)] ^ S8[gb(x, 0xd)]
		i++
	}
	c.keylen = keylen
}

func (c *Cast5Cipher) Encrypt(data []byte) ([]byte, error) {
	blockSize := c.BlockSize()
	if len(data)%blockSize != 0 {
		return nil, ErrInvalidBlockSize
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += blockSize {
		L := loadU32BE(data[i : i+4])
		R := loadU32BE(data[i+4 : i+8])
		L ^= cast5FI(R, c.K[0], c.K[16])
		R ^= cast5FII(L, c.K[1], c.K[17])
		L ^= cast5FIII(R, c.K[2], c.K[18])
		R ^= cast5FI(L, c.K[3], c.K[19])
		L ^= cast5FII(R, c.K[4], c.K[20])
		R ^= cast5FIII(L, c.K[5], c.K[21])
		L ^= cast5FI(R, c.K[6], c.K[22])
		R ^= cast5FII(L, c.K[7], c.K[23])
		L ^= cast5FIII(R, c.K[8], c.K[24])
		R ^= cast5FI(L, c.K[9], c.K[25])
		L ^= cast5FII(R, c.K[10], c.K[26])
		R ^= cast5FIII(L, c.K[11], c.K[27])
		if c.keylen > 10 {
			L ^= cast5FI(R, c.K[12], c.K[28])
			R ^= cast5FII(L, c.K[13], c.K[29])
			L ^= cast5FIII(R, c.K[14], c.K[30])
			R ^= cast5FI(L, c.K[15], c.K[31])
		}
		storeU32BE(out[i:i+4], R)
		storeU32BE(out[i+4:i+8], L)
	}
	return out, nil
}

func (c *Cast5Cipher) Decrypt(data []byte) ([]byte, error) {
	blockSize := c.BlockSize()
	if len(data)%blockSize != 0 {
		return nil, ErrInvalidBlockSize
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += blockSize {
		R := loadU32BE(data[i : i+4])
		L := loadU32BE(data[i+4 : i+8])
		if c.keylen > 10 {
			R ^= cast5FI(L, c.K[15], c.K[31])
			L ^= cast5FIII(R, c.K[14], c.K[30])
			R ^= cast5FII(L, c.K[13], c.K[29])
			L ^= cast5FI(R, c.K[12], c.K[28])
		}
		R ^= cast5FIII(L, c.K[11], c.K[27])
		L ^= cast5FII(R, c.K[10], c.K[26])
		R ^= cast5FI(L, c.K[9], c.K[25])
		L ^= cast5FIII(R, c.K[8], c.K[24])
		R ^= cast5FII(L, c.K[7], c.K[23])
		L ^= cast5FI(R, c.K[6], c.K[22])
		R ^= cast5FIII(L, c.K[5], c.K[21])
		L ^= cast5FII(R, c.K[4], c.K[20])
		R ^= cast5FI(L, c.K[3], c.K[19])
		L ^= cast5FIII(R, c.K[2], c.K[18])
		R ^= cast5FII(L, c.K[1], c.K[17])
		L ^= cast5FI(R, c.K[0], c.K[16])
		storeU32BE(out[i:i+4], L)
		storeU32BE(out[i+4:i+8], R)
	}
	return out, nil
}
