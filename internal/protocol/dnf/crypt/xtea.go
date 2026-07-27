package crypt

type XteaCipher struct {
	A [32]uint32
	B [32]uint32
}

func NewXteaCipher() *XteaCipher {
	return &XteaCipher{}
}

func (x *XteaCipher) SetKey(key []byte) error {
	if len(key) != 16 {
		return ErrInvalidKeySize
	}
	K0 := loadU32LE(key[0:4])
	K1 := loadU32LE(key[4:8])
	K2 := loadU32LE(key[8:12])
	K3 := loadU32LE(key[12:16])
	sum := uint32(0)
	for i := uint32(0); i < 32; i++ {
		x.A[i] = (sum + [4]uint32{K0, K1, K2, K3}[sum&3])
		sum += 0x9E3779B9
		x.B[i] = (sum + [4]uint32{K0, K1, K2, K3}[(sum>>11)&3])
	}
	return nil
}

func (x *XteaCipher) KeySize() int {
	return 16
}

func (x *XteaCipher) BlockSize() int {
	return 8
}

func (x *XteaCipher) Encrypt(data []byte) ([]byte, error) {
	blockSize := x.BlockSize()
	if len(data)%blockSize != 0 {
		return nil, ErrInvalidBlockSize
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += blockSize {
		y := loadU32LE(data[i : i+4])
		z := loadU32LE(data[i+4 : i+8])
		for r := 0; r < 32; r += 4 {
			y += (((z << 4) ^ (z >> 5)) + z) ^ x.A[r]
			z += (((y << 4) ^ (y >> 5)) + y) ^ x.B[r]
			y += (((z << 4) ^ (z >> 5)) + z) ^ x.A[r+1]
			z += (((y << 4) ^ (y >> 5)) + y) ^ x.B[r+1]
			y += (((z << 4) ^ (z >> 5)) + z) ^ x.A[r+2]
			z += (((y << 4) ^ (y >> 5)) + y) ^ x.B[r+2]
			y += (((z << 4) ^ (z >> 5)) + z) ^ x.A[r+3]
			z += (((y << 4) ^ (y >> 5)) + y) ^ x.B[r+3]
		}
		storeU32LE(out[i:i+4], y)
		storeU32LE(out[i+4:i+8], z)
	}
	return out, nil
}

func (x *XteaCipher) Decrypt(data []byte) ([]byte, error) {
	blockSize := x.BlockSize()
	if len(data)%blockSize != 0 {
		return nil, ErrInvalidBlockSize
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += blockSize {
		y := loadU32LE(data[i : i+4])
		z := loadU32LE(data[i+4 : i+8])
		for r := 31; r >= 0; r -= 4 {
			z -= (((y << 4) ^ (y >> 5)) + y) ^ x.B[r]
			y -= (((z << 4) ^ (z >> 5)) + z) ^ x.A[r]
			z -= (((y << 4) ^ (y >> 5)) + y) ^ x.B[r-1]
			y -= (((z << 4) ^ (z >> 5)) + z) ^ x.A[r-1]
			z -= (((y << 4) ^ (y >> 5)) + y) ^ x.B[r-2]
			y -= (((z << 4) ^ (z >> 5)) + z) ^ x.A[r-2]
			z -= (((y << 4) ^ (y >> 5)) + y) ^ x.B[r-3]
			y -= (((z << 4) ^ (z >> 5)) + z) ^ x.A[r-3]
		}
		storeU32LE(out[i:i+4], y)
		storeU32LE(out[i+4:i+8], z)
	}
	return out, nil
}
