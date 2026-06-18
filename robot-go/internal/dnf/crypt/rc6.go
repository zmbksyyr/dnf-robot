package crypt

type Rc6Cipher struct {
	S [44]byte
}

func NewRc6Cipher() *Rc6Cipher {
	return &Rc6Cipher{}
}

func (r *Rc6Cipher) SetKey(key []byte) error {
	if len(key) < 16 {
		return ErrInvalidKeySize
	}
	// [C++->Go] DNF uses first 32 bytes of the 60-byte key slot
	// memcpy(RC6, key, 32); rc6_key_setup((uint64_t)RC6, 32);
	klen := len(key)
	if klen > 32 {
		klen = 32
	}
	r.keySetup(key[:klen])
	return nil
}

func (r *Rc6Cipher) KeySize() int   { return 60 }
func (r *Rc6Cipher) BlockSize() int { return 16 }

func ror32b(v uint32, n int) uint32 {
	n &= 31
	return (v >> n) | (v << (32 - n))
}

func rol32b(v uint32, n int) uint32 {
	n &= 31
	return (v << n) | (v >> (32 - n))
}

func (r *Rc6Cipher) keySetup(key []byte) {
	keyWords := (len(key) + 3) / 4
	v7 := make([]uint32, keyWords)
	for i := len(key) - 1; i >= 0; i-- {
		v7[i/4] = (v7[i/4] << 8) + uint32(key[i])
	}
	// [C++->Go] unsigned char S[44]; *v8 = 99; S[i]=S[i-1]-71
	r.S[0] = 99
	for i := 1; i <= 43; i++ {
		r.S[i] = r.S[i-1] - 71
	}
	v11 := 0
	idx := 0
	v15 := uint32(0)
	v14 := uint32(0)
	limit := 44
	if keyWords > 44 {
		limit = keyWords
	}
	limit *= 3
	for j := 1; j <= limit; j++ {
		// [C++->Go] v3 = __ROR4__(v15+v14+v8[idx], 29)
		v3 := ror32b(v15+v14+uint32(r.S[idx]), 29)
		r.S[idx] = byte(v3)    // *v2 = v3  (truncates to byte)
		v14 = uint32(r.S[idx]) // v14 = *v2 (reads TRUNCATED byte!)
		v4 := v11
		shift := int((v14 + v15) & 0x1F)
		// [C++->Go] v7[v11] = ROL(v15+v14+v7[v11], shift)
		v7[v11] = rol32b(v15+v14+v7[v11], shift)
		v15 = v7[v4]
		v5 := idx + 1
		idx = v5 % 44
		v11 = (v11 + 1) % keyWords
	}
}

func (r *Rc6Cipher) Encrypt(data []byte) ([]byte, error) {
	blockSize := r.BlockSize()
	if len(data)%blockSize != 0 {
		return nil, ErrInvalidBlockSize
	}
	out := make([]byte, len(data))
	// [C++->Go] S[i] read as unsigned char (1 byte each)
	S := r.S[:]
	for i := 0; i < len(data); i += blockSize {
		v10 := loadU32LE(data[i : i+4])
		v12 := loadU32LE(data[i+8 : i+12])
		v11 := uint32(S[0]) + loadU32LE(data[i+4:i+8])
		v13 := uint32(S[1]) + loadU32LE(data[i+12:i+16])
		for j := 2; j <= 40; j += 2 {
			v3 := ror32b(v11*(2*v11+1), 27)
			v5 := ror32b(v13*(2*v13+1), 27)
			v10, v11, v12, v13 = v11,
				rol32b(v5^v12, int(v3&0x1F))+uint32(S[j+1]),
				v13,
				rol32b(v3^v10, int(v5&0x1F))+uint32(S[j])
		}
		storeU32LE(out[i:i+4], uint32(S[42])+v10)
		storeU32LE(out[i+4:i+8], v11)
		storeU32LE(out[i+8:i+12], uint32(S[43])+v12)
		storeU32LE(out[i+12:i+16], v13)
	}
	return out, nil
}

func (r *Rc6Cipher) Decrypt(data []byte) ([]byte, error) {
	blockSize := r.BlockSize()
	if len(data)%blockSize != 0 {
		return nil, ErrInvalidBlockSize
	}
	out := make([]byte, len(data))
	S := r.S[:]
	for i := 0; i < len(data); i += blockSize {
		v13 := loadU32LE(data[i+4 : i+8])
		v15 := loadU32LE(data[i+12 : i+16])
		v14 := loadU32LE(data[i+8:i+12]) - uint32(S[43])
		v12 := loadU32LE(data[i:i+4]) - uint32(S[42])
		for j := 40; j > 1; j -= 2 {
			v3 := v15
			v15 = v14
			v4 := v13
			v13 = v12
			v5 := ror32b(v15*(2*v15+1), 27)
			v7 := ror32b(v13*(2*v13+1), 27)
			// [C++->Go] ROR(v4 - S[j+1], v7&0x1F) via right-shift+left-shift pattern
			v14 = v5 ^ ror32b(v4-uint32(S[j+1]), int(v7&0x1F))
			v12 = v7 ^ ror32b(v3-uint32(S[j]), int(v5&0x1F))
		}
		storeU32LE(out[i:i+4], v12)
		storeU32LE(out[i+4:i+8], v13-uint32(S[0]))
		storeU32LE(out[i+8:i+12], v14)
		storeU32LE(out[i+12:i+16], v15-uint32(S[1]))
	}
	return out, nil
}
