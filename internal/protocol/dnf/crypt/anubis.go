package crypt

type AnubisCipher struct {
	R           int
	roundKeyEnc [19][4]uint32
	roundKeyDec [19][4]uint32
}

func NewAnubisCipher() *AnubisCipher {
	return &AnubisCipher{}
}

func (a *AnubisCipher) SetKey(key []byte) error {
	if len(key) < 16 || len(key) > 40 {
		return ErrInvalidKeySize
	}
	keyLen := len(key)
	var keyBits int
	switch keyLen {
	case 16:
		keyBits = 128
	case 20:
		keyBits = 160
	case 24:
		keyBits = 192
	case 28:
		keyBits = 224
	case 32:
		keyBits = 256
	case 36:
		keyBits = 288
	case 40:
		keyBits = 320
	default:
		return ErrInvalidKeySize
	}
	a.R = 8 + keyBits/32
	N := keyBits / 32

	var kappa [10]uint32
	for i := 0; i < N && i < 10; i++ {
		kappa[i] = loadU32BE(key[4*i : 4*i+4])
	}

	for i := 0; i <= a.R; i++ {
		for j := 0; j < 4; j++ {
			a.roundKeyEnc[i][j] = 0
		}
	}
	for i := 0; i <= a.R; i++ {
		idx := i % N
		if idx < N {
			for j := 0; j < 4; j++ {
				a.roundKeyEnc[i][j] ^= kappa[(idx+j)%10]
			}
		}
	}

	for i := 0; i <= a.R; i++ {
		for j := 0; j < 4; j++ {
			v := a.roundKeyEnc[i][j]
			a.roundKeyEnc[i][j] = anubisT0[byte(v>>24)] ^ anubisT1[byte(v>>16)] ^ anubisT2[byte(v>>8)] ^ anubisT3[byte(v)]
		}
	}

	if a.R > 0 {
		for r := 0; r < a.R; r++ {
			for j := 0; j < 4; j++ {
				a.roundKeyDec[r][j] = a.roundKeyEnc[a.R-r][j]
			}
		}
		for j := 0; j < 4; j++ {
			a.roundKeyDec[a.R][j] = a.roundKeyEnc[0][j]
		}
		for r := 1; r < a.R; r++ {
			for j := 0; j < 4; j++ {
				v := a.roundKeyDec[r][j]
				a.roundKeyDec[r][j] = anubisT4[byte(v>>24)] ^ anubisT5[byte(v>>16)] ^ anubisT4[byte(v>>8)] ^ anubisT5[byte(v)]
			}
		}
	}
	return nil
}

func (a *AnubisCipher) KeySize() int {
	return 16
}

func (a *AnubisCipher) BlockSize() int {
	return 16
}

func anubisGamma(state *[4]uint32) {
	t0 := anubisT0[byte(state[0]>>24)] ^ anubisT1[byte(state[1]>>16)] ^ anubisT2[byte(state[2]>>8)] ^ anubisT3[byte(state[3])]
	t1 := anubisT0[byte(state[1]>>24)] ^ anubisT1[byte(state[2]>>16)] ^ anubisT2[byte(state[3]>>8)] ^ anubisT3[byte(state[0])]
	t2 := anubisT0[byte(state[2]>>24)] ^ anubisT1[byte(state[3]>>16)] ^ anubisT2[byte(state[0]>>8)] ^ anubisT3[byte(state[1])]
	t3 := anubisT0[byte(state[3]>>24)] ^ anubisT1[byte(state[0]>>16)] ^ anubisT2[byte(state[1]>>8)] ^ anubisT3[byte(state[2])]
	state[0] = t0
	state[1] = t1
	state[2] = t2
	state[3] = t3
}

func anubisGammaInv(state *[4]uint32) {
	t0 := anubisT4[byte(state[0]>>24)] ^ anubisT5[byte(state[3]>>16)] ^ anubisT4[byte(state[2]>>8)] ^ anubisT5[byte(state[1])]
	t1 := anubisT4[byte(state[1]>>24)] ^ anubisT5[byte(state[0]>>16)] ^ anubisT4[byte(state[3]>>8)] ^ anubisT5[byte(state[2])]
	t2 := anubisT4[byte(state[2]>>24)] ^ anubisT5[byte(state[1]>>16)] ^ anubisT4[byte(state[0]>>8)] ^ anubisT5[byte(state[3])]
	t3 := anubisT4[byte(state[3]>>24)] ^ anubisT5[byte(state[2]>>16)] ^ anubisT4[byte(state[1]>>8)] ^ anubisT5[byte(state[0])]
	state[0] = t0
	state[1] = t1
	state[2] = t2
	state[3] = t3
}

func (a *AnubisCipher) Encrypt(data []byte) ([]byte, error) {
	blockSize := a.BlockSize()
	if len(data)%blockSize != 0 {
		return nil, ErrInvalidBlockSize
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += blockSize {
		var s [4]uint32
		s[0] = loadU32BE(data[i : i+4])
		s[1] = loadU32BE(data[i+4 : i+8])
		s[2] = loadU32BE(data[i+8 : i+12])
		s[3] = loadU32BE(data[i+12 : i+16])
		s[0] ^= a.roundKeyEnc[0][0]
		s[1] ^= a.roundKeyEnc[0][1]
		s[2] ^= a.roundKeyEnc[0][2]
		s[3] ^= a.roundKeyEnc[0][3]
		for r := 1; r < a.R; r++ {
			anubisGamma(&s)
			s[0] ^= a.roundKeyEnc[r][0]
			s[1] ^= a.roundKeyEnc[r][1]
			s[2] ^= a.roundKeyEnc[r][2]
			s[3] ^= a.roundKeyEnc[r][3]
		}
		s0 := anubisT4[byte(s[0]>>24)] ^ anubisT5[byte(s[1]>>16)] ^ anubisT4[byte(s[2]>>8)] ^ anubisT5[byte(s[3])] ^ a.roundKeyEnc[a.R][0]
		s1 := anubisT4[byte(s[1]>>24)] ^ anubisT5[byte(s[2]>>16)] ^ anubisT4[byte(s[3]>>8)] ^ anubisT5[byte(s[0])] ^ a.roundKeyEnc[a.R][1]
		s2 := anubisT4[byte(s[2]>>24)] ^ anubisT5[byte(s[3]>>16)] ^ anubisT4[byte(s[0]>>8)] ^ anubisT5[byte(s[1])] ^ a.roundKeyEnc[a.R][2]
		s3 := anubisT4[byte(s[3]>>24)] ^ anubisT5[byte(s[0]>>16)] ^ anubisT4[byte(s[1]>>8)] ^ anubisT5[byte(s[2])] ^ a.roundKeyEnc[a.R][3]
		storeU32BE(out[i:i+4], s0)
		storeU32BE(out[i+4:i+8], s1)
		storeU32BE(out[i+8:i+12], s2)
		storeU32BE(out[i+12:i+16], s3)
	}
	return out, nil
}

func (a *AnubisCipher) Decrypt(data []byte) ([]byte, error) {
	blockSize := a.BlockSize()
	if len(data)%blockSize != 0 {
		return nil, ErrInvalidBlockSize
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += blockSize {
		var s [4]uint32
		s[0] = loadU32BE(data[i : i+4])
		s[1] = loadU32BE(data[i+4 : i+8])
		s[2] = loadU32BE(data[i+8 : i+12])
		s[3] = loadU32BE(data[i+12 : i+16])
		s[0] ^= a.roundKeyDec[0][0]
		s[1] ^= a.roundKeyDec[0][1]
		s[2] ^= a.roundKeyDec[0][2]
		s[3] ^= a.roundKeyDec[0][3]
		for r := 1; r < a.R; r++ {
			anubisGammaInv(&s)
			s[0] ^= a.roundKeyDec[r][0]
			s[1] ^= a.roundKeyDec[r][1]
			s[2] ^= a.roundKeyDec[r][2]
			s[3] ^= a.roundKeyDec[r][3]
		}
		s0 := anubisT4[byte(s[0]>>24)] ^ anubisT5[byte(s[3]>>16)] ^ anubisT4[byte(s[2]>>8)] ^ anubisT5[byte(s[1])] ^ a.roundKeyDec[a.R][0]
		s1 := anubisT4[byte(s[1]>>24)] ^ anubisT5[byte(s[0]>>16)] ^ anubisT4[byte(s[3]>>8)] ^ anubisT5[byte(s[2])] ^ a.roundKeyDec[a.R][1]
		s2 := anubisT4[byte(s[2]>>24)] ^ anubisT5[byte(s[1]>>16)] ^ anubisT4[byte(s[0]>>8)] ^ anubisT5[byte(s[3])] ^ a.roundKeyDec[a.R][2]
		s3 := anubisT4[byte(s[3]>>24)] ^ anubisT5[byte(s[2]>>16)] ^ anubisT4[byte(s[1]>>8)] ^ anubisT5[byte(s[0])] ^ a.roundKeyDec[a.R][3]
		storeU32BE(out[i:i+4], s0)
		storeU32BE(out[i+4:i+8], s1)
		storeU32BE(out[i+8:i+12], s2)
		storeU32BE(out[i+12:i+16], s3)
	}
	return out, nil
}
