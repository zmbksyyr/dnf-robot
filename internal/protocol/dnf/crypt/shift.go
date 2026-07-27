package crypt

type ShiftCipher struct {
	key [2]uint32
}

func NewShiftCipher() *ShiftCipher {
	return &ShiftCipher{}
}

func (s *ShiftCipher) SetKey(key []byte) error {
	if len(key) < 8 {
		return ErrInvalidKeySize
	}
	s.key[0] = uint32(key[0]) | uint32(key[1])<<8 | uint32(key[2])<<16 | uint32(key[3])<<24
	s.key[0] &= 0x1F
	s.key[1] = uint32(key[4]) | uint32(key[5])<<8 | uint32(key[6])<<16 | uint32(key[7])<<24
	return nil
}

func (s *ShiftCipher) KeySize() int {
	return 8
}

func (s *ShiftCipher) BlockSize() int {
	return 4
}

func (s *ShiftCipher) Encrypt(data []byte) ([]byte, error) {
	if s.key[0] > 0x1F {
		return nil, ErrInvalidKeySize
	}
	blockSize := s.BlockSize()
	if len(data)%blockSize != 0 {
		return nil, ErrInvalidBlockSize
	}
	out := make([]byte, len(data))
	xorKey := s.key[1]
	for i := 0; i < len(data); i += blockSize {
		v := uint32(data[i]) | uint32(data[i+1])<<8 | uint32(data[i+2])<<16 | uint32(data[i+3])<<24
		v ^= xorKey
		out[i] = byte(v)
		out[i+1] = byte(v >> 8)
		out[i+2] = byte(v >> 16)
		out[i+3] = byte(v >> 24)
	}
	return out, nil
}

func (s *ShiftCipher) Decrypt(data []byte) ([]byte, error) {
	if s.key[0] > 0x1F {
		return nil, ErrInvalidKeySize
	}
	blockSize := s.BlockSize()
	if len(data)%blockSize != 0 {
		return nil, ErrInvalidBlockSize
	}
	out := make([]byte, len(data))
	xorKey := s.key[1]
	for i := 0; i < len(data); i += blockSize {
		v := uint32(data[i]) | uint32(data[i+1])<<8 | uint32(data[i+2])<<16 | uint32(data[i+3])<<24
		v = xorKey ^ v
		out[i] = byte(v)
		out[i+1] = byte(v >> 8)
		out[i+2] = byte(v >> 16)
		out[i+3] = byte(v >> 24)
	}
	return out, nil
}
