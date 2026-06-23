package crypt

type KhazadCipher struct {
	roundKeyEnc [9]uint64
	roundKeyDec [9]uint64
}

var khazadC = [9]uint64{
	0xba542f7453d3d24d,
	0x50ac8dbf70529a4c,
	0xead597d133515ba6,
	0xde48a899db32b7fc,
	0xe39e919be2bb416e,
	0xa5cb6b95a1f3b102,
	0xccc41d14c363da5d,
	0x5fdc7dcd7f5a6c5c,
	0xf726ffede89d6f8e,
}

func NewKhazadCipher() *KhazadCipher {
	return &KhazadCipher{}
}

func (k *KhazadCipher) SetKey(key []byte) error {
	if len(key) != 16 {
		return ErrInvalidKeySize
	}
	k2 := loadU64BE(key[0:8])
	k1 := loadU64BE(key[8:16])
	for r := 0; r <= 8; r++ {
		k.roundKeyEnc[r] = khazadRound(k1) ^ khazadC[r] ^ k2
		k2 = k1
		k1 = k.roundKeyEnc[r]
	}

	k.roundKeyDec[0] = k.roundKeyEnc[8]
	for r := 1; r < 8; r++ {
		k1 = k.roundKeyEnc[8-r]
		k.roundKeyDec[r] =
			T0[byte(T7[byte(k1>>56)])] ^
				T1[byte(T7[byte(k1>>48)])] ^
				T2[byte(T7[byte(k1>>40)])] ^
				T3[byte(T7[byte(k1>>32)])] ^
				T4[byte(T7[byte(k1>>24)])] ^
				T5[byte(T7[byte(k1>>16)])] ^
				T6[byte(T7[byte(k1>>8)])] ^
				T7[byte(T7[byte(k1)])]
	}
	k.roundKeyDec[8] = k.roundKeyEnc[0]
	return nil
}

func (k *KhazadCipher) KeySize() int {
	return 16
}

func (k *KhazadCipher) BlockSize() int {
	return 8
}

func (k *KhazadCipher) Encrypt(data []byte) ([]byte, error) {
	blockSize := k.BlockSize()
	if len(data)%blockSize != 0 {
		return nil, ErrInvalidBlockSize
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += blockSize {
		storeU64BE(out[i:i+8], khazadCryptBlock(loadU64BE(data[i:i+8]), &k.roundKeyEnc))
	}
	return out, nil
}

func (k *KhazadCipher) Decrypt(data []byte) ([]byte, error) {
	blockSize := k.BlockSize()
	if len(data)%blockSize != 0 {
		return nil, ErrInvalidBlockSize
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += blockSize {
		storeU64BE(out[i:i+8], khazadCryptBlock(loadU64BE(data[i:i+8]), &k.roundKeyDec))
	}
	return out, nil
}

func khazadRound(state uint64) uint64 {
	return T0[byte(state>>56)] ^
		T1[byte(state>>48)] ^
		T2[byte(state>>40)] ^
		T3[byte(state>>32)] ^
		T4[byte(state>>24)] ^
		T5[byte(state>>16)] ^
		T6[byte(state>>8)] ^
		T7[byte(state)]
}

func khazadCryptBlock(block uint64, roundKey *[9]uint64) uint64 {
	state := block ^ roundKey[0]
	for r := 1; r < 8; r++ {
		state = khazadRound(state) ^ roundKey[r]
	}
	return (T0[byte(state>>56)] & 0xff00000000000000) ^
		(T1[byte(state>>48)] & 0x00ff000000000000) ^
		(T2[byte(state>>40)] & 0x0000ff0000000000) ^
		(T3[byte(state>>32)] & 0x000000ff00000000) ^
		(T4[byte(state>>24)] & 0x00000000ff000000) ^
		(T5[byte(state>>16)] & 0x0000000000ff0000) ^
		(T6[byte(state>>8)] & 0x000000000000ff00) ^
		(T7[byte(state)] & 0x00000000000000ff) ^
		roundKey[8]
}
