package crypt

type TeaCipher struct {
	key   [4]uint32
	chain [8]byte
	mode  int
}

func NewTeaCipher() *TeaCipher {
	return &TeaCipher{}
}

func (t *TeaCipher) SetKey(key []byte) error {
	if len(key) < 16 {
		return ErrInvalidKeySize
	}
	t.key[0] = loadU32BE(key[0:4])
	t.key[1] = loadU32BE(key[4:8])
	t.key[2] = loadU32BE(key[8:12])
	t.key[3] = loadU32BE(key[12:16])
	t.mode = ECB
	return nil
}

func (t *TeaCipher) KeySize() int {
	return 16
}

func (t *TeaCipher) BlockSize() int {
	return 8
}

func (t *TeaCipher) ResetChain() {
	for i := range t.chain {
		t.chain[i] = 0
	}
}

func (t *TeaCipher) SetMode(mode int) {
	t.mode = mode
}

func (t *TeaCipher) encryptBlock(in, out []byte) {
	y := loadU32BE(in[0:4])
	z := loadU32BE(in[4:8])
	sum := uint32(0)
	delta := uint32(0x9E3779B9)
	for n := 0; n < 32; n++ {
		sum += delta
		y += ((z << 4) ^ (z >> 5)) + z ^ sum + t.key[sum&3]
		z += ((y << 4) ^ (y >> 5)) + y ^ sum + t.key[(sum>>11)&3]
	}
	storeU32BE(out[0:4], y)
	storeU32BE(out[4:8], z)
}

func (t *TeaCipher) decryptBlock(in, out []byte) {
	y := loadU32BE(in[0:4])
	z := loadU32BE(in[4:8])
	sum := uint32(0xC6EF3720)
	delta := uint32(0x9E3779B9)
	for n := 0; n < 32; n++ {
		z -= ((y << 4) ^ (y >> 5)) + y ^ sum + t.key[(sum>>11)&3]
		sum -= delta
		y -= ((z << 4) ^ (z >> 5)) + z ^ sum + t.key[sum&3]
	}
	storeU32BE(out[0:4], y)
	storeU32BE(out[4:8], z)
}

func (t *TeaCipher) Encrypt(data []byte) ([]byte, error) {
	blockSize := t.BlockSize()
	if len(data)%blockSize != 0 {
		return nil, ErrInvalidBlockSize
	}
	out := make([]byte, len(data))
	chain := make([]byte, blockSize)
	copy(chain, t.chain[:])
	switch t.mode {
	case CBC:
		for i := 0; i < len(data); i += blockSize {
			for j := 0; j < blockSize; j++ {
				chain[j] ^= data[i+j]
			}
			t.encryptBlock(chain, out[i:i+blockSize])
			copy(chain, out[i:i+blockSize])
		}
	case CFB:
		for i := 0; i < len(data); i += blockSize {
			t.encryptBlock(chain, out[i:i+blockSize])
			for j := 0; j < blockSize; j++ {
				out[i+j] ^= data[i+j]
			}
			copy(chain, out[i:i+blockSize])
		}
	default:
		for i := 0; i < len(data); i += blockSize {
			t.encryptBlock(data[i:i+blockSize], out[i:i+blockSize])
		}
	}
	return out, nil
}

func (t *TeaCipher) Decrypt(data []byte) ([]byte, error) {
	blockSize := t.BlockSize()
	if len(data)%blockSize != 0 {
		return nil, ErrInvalidBlockSize
	}
	out := make([]byte, len(data))
	chain := make([]byte, blockSize)
	copy(chain, t.chain[:])
	switch t.mode {
	case CBC:
		for i := 0; i < len(data); i += blockSize {
			t.decryptBlock(data[i:i+blockSize], out[i:i+blockSize])
			for j := 0; j < blockSize; j++ {
				out[i+j] ^= chain[j]
			}
			copy(chain, data[i:i+blockSize])
		}
	case CFB:
		for i := 0; i < len(data); i += blockSize {
			t.encryptBlock(chain, out[i:i+blockSize])
			for j := 0; j < blockSize; j++ {
				out[i+j] ^= data[i+j]
			}
			copy(chain, data[i:i+blockSize])
		}
	default:
		for i := 0; i < len(data); i += blockSize {
			t.decryptBlock(data[i:i+blockSize], out[i:i+blockSize])
		}
	}
	return out, nil
}
