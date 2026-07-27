package crypt

type AnubisCipher struct {
	rounds      int
	roundKeyEnc [19][4]uint32
	roundKeyDec [19][4]uint32
}

var anubisRoundConstants = [...]uint32{
	0xa7d3e671, 0xd0ac4d79, 0x3ac991fc, 0x1e4754bd,
	0x8ca57afb, 0x63b8ddd4, 0xe5b3c5be, 0xa9880ca2,
	0x39df29da, 0x2ba8cb4c, 0x4b22aa24, 0x4170a6f9,
	0x5ae2b036, 0x7de433ff, 0x6020088b, 0x5eab7f78,
	0x7c2c57d2, 0xdc6d7e0d, 0x5394c328,
}

func NewAnubisCipher() *AnubisCipher {
	return &AnubisCipher{}
}

func (a *AnubisCipher) SetKey(key []byte) error {
	if len(key) < 16 || len(key) > 40 || len(key)%4 != 0 {
		return ErrInvalidKeySize
	}

	n := len(key) / 4
	a.rounds = 8 + n
	var kappa [10]uint32
	var intermediate [10]uint32
	for i := 0; i < n; i++ {
		kappa[i] = loadU32BE(key[4*i : 4*i+4])
	}

	for round := 0; round <= a.rounds; round++ {
		k0 := anubisT4[byte(kappa[n-1]>>24)]
		k1 := anubisT4[byte(kappa[n-1]>>16)]
		k2 := anubisT4[byte(kappa[n-1]>>8)]
		k3 := anubisT4[byte(kappa[n-1])]
		for i := n - 2; i >= 0; i-- {
			k0 = anubisT4[byte(kappa[i]>>24)] ^ anubisKeyTransform(k0)
			k1 = anubisT4[byte(kappa[i]>>16)] ^ anubisKeyTransform(k1)
			k2 = anubisT4[byte(kappa[i]>>8)] ^ anubisKeyTransform(k2)
			k3 = anubisT4[byte(kappa[i])] ^ anubisKeyTransform(k3)
		}
		a.roundKeyEnc[round] = [4]uint32{k0, k1, k2, k3}
		if round == a.rounds {
			break
		}

		for i := 0; i < n; i++ {
			j := i
			intermediate[i] = anubisT0[byte(kappa[j]>>24)]
			j = (j - 1 + n) % n
			intermediate[i] ^= anubisT1[byte(kappa[j]>>16)]
			j = (j - 1 + n) % n
			intermediate[i] ^= anubisT2[byte(kappa[j]>>8)]
			j = (j - 1 + n) % n
			intermediate[i] ^= anubisT3[byte(kappa[j])]
		}
		kappa[0] = intermediate[0] ^ anubisRoundConstants[round]
		copy(kappa[1:n], intermediate[1:n])
	}

	for i := 0; i < 4; i++ {
		a.roundKeyDec[0][i] = a.roundKeyEnc[a.rounds][i]
		a.roundKeyDec[a.rounds][i] = a.roundKeyEnc[0][i]
	}
	for round := 1; round < a.rounds; round++ {
		for i := 0; i < 4; i++ {
			v := a.roundKeyEnc[a.rounds-round][i]
			a.roundKeyDec[round][i] = anubisT0[byte(anubisT4[byte(v>>24)])] ^
				anubisT1[byte(anubisT4[byte(v>>16)])] ^
				anubisT2[byte(anubisT4[byte(v>>8)])] ^
				anubisT3[byte(anubisT4[byte(v)])]
		}
	}
	return nil
}

func anubisKeyTransform(v uint32) uint32 {
	return anubisT5[byte(v>>24)]&0xff000000 |
		anubisT5[byte(v>>16)]&0x00ff0000 |
		anubisT5[byte(v>>8)]&0x0000ff00 |
		anubisT5[byte(v)]&0x000000ff
}

func (a *AnubisCipher) KeySize() int {
	return 16
}

func (a *AnubisCipher) BlockSize() int {
	return 16
}

func (a *AnubisCipher) Encrypt(data []byte) ([]byte, error) {
	return a.crypt(data, &a.roundKeyEnc)
}

func (a *AnubisCipher) Decrypt(data []byte) ([]byte, error) {
	return a.crypt(data, &a.roundKeyDec)
}

func (a *AnubisCipher) crypt(data []byte, roundKeys *[19][4]uint32) ([]byte, error) {
	if a.rounds == 0 {
		return nil, ErrNotInitialized
	}
	if len(data)%a.BlockSize() != 0 {
		return nil, ErrInvalidBlockSize
	}

	out := make([]byte, len(data))
	for offset := 0; offset < len(data); offset += a.BlockSize() {
		var state [4]uint32
		for i := 0; i < 4; i++ {
			state[i] = loadU32BE(data[offset+4*i:offset+4*i+4]) ^ roundKeys[0][i]
		}
		for round := 1; round < a.rounds; round++ {
			state = [4]uint32{
				anubisT0[byte(state[0]>>24)] ^ anubisT1[byte(state[1]>>24)] ^ anubisT2[byte(state[2]>>24)] ^ anubisT3[byte(state[3]>>24)] ^ roundKeys[round][0],
				anubisT0[byte(state[0]>>16)] ^ anubisT1[byte(state[1]>>16)] ^ anubisT2[byte(state[2]>>16)] ^ anubisT3[byte(state[3]>>16)] ^ roundKeys[round][1],
				anubisT0[byte(state[0]>>8)] ^ anubisT1[byte(state[1]>>8)] ^ anubisT2[byte(state[2]>>8)] ^ anubisT3[byte(state[3]>>8)] ^ roundKeys[round][2],
				anubisT0[byte(state[0])] ^ anubisT1[byte(state[1])] ^ anubisT2[byte(state[2])] ^ anubisT3[byte(state[3])] ^ roundKeys[round][3],
			}
		}
		last := [4]uint32{
			anubisT0[byte(state[0]>>24)]&0xff000000 ^ anubisT1[byte(state[1]>>24)]&0x00ff0000 ^ anubisT2[byte(state[2]>>24)]&0x0000ff00 ^ anubisT3[byte(state[3]>>24)]&0x000000ff ^ roundKeys[a.rounds][0],
			anubisT0[byte(state[0]>>16)]&0xff000000 ^ anubisT1[byte(state[1]>>16)]&0x00ff0000 ^ anubisT2[byte(state[2]>>16)]&0x0000ff00 ^ anubisT3[byte(state[3]>>16)]&0x000000ff ^ roundKeys[a.rounds][1],
			anubisT0[byte(state[0]>>8)]&0xff000000 ^ anubisT1[byte(state[1]>>8)]&0x00ff0000 ^ anubisT2[byte(state[2]>>8)]&0x0000ff00 ^ anubisT3[byte(state[3]>>8)]&0x000000ff ^ roundKeys[a.rounds][2],
			anubisT0[byte(state[0])]&0xff000000 ^ anubisT1[byte(state[1])]&0x00ff0000 ^ anubisT2[byte(state[2])]&0x0000ff00 ^ anubisT3[byte(state[3])]&0x000000ff ^ roundKeys[a.rounds][3],
		}
		for i := 0; i < 4; i++ {
			storeU32BE(out[offset+4*i:offset+4*i+4], last[i])
		}
	}
	return out, nil
}
