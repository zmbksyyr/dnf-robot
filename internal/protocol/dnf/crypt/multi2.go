package crypt

import (
	"encoding/binary"
	"math/bits"
)

const multi2Rounds = 128

type Multi2Cipher struct {
	N  int
	uk [8]uint32
}

func NewMulti2Cipher() *Multi2Cipher {
	return &Multi2Cipher{}
}

func (m *Multi2Cipher) SetKey(key []byte) error {
	if len(key) != m.KeySize() {
		return ErrInvalidKeySize
	}

	var systemKey [8]uint32
	for i := range systemKey {
		systemKey[i] = binary.BigEndian.Uint32(key[i*4 : i*4+4])
	}
	dataKey := [2]uint32{
		binary.BigEndian.Uint32(key[32:36]),
		binary.BigEndian.Uint32(key[36:40]),
	}

	m.N = multi2Rounds
	m.uk = multi2Setup(dataKey, systemKey)
	return nil
}

func (m *Multi2Cipher) KeySize() int {
	return 40
}

func (m *Multi2Cipher) BlockSize() int {
	return 8
}

func multi2Pi1(p *[2]uint32) {
	p[1] ^= p[0]
}

func multi2Pi2(p *[2]uint32, key []uint32) {
	t := p[1] + key[0]
	t = bits.RotateLeft32(t, 1) + t - 1
	t = bits.RotateLeft32(t, 4) ^ t
	p[0] ^= t
}

func multi2Pi3(p *[2]uint32, key []uint32) {
	t := p[0] + key[1]
	t = bits.RotateLeft32(t, 2) + t + 1
	t = bits.RotateLeft32(t, 8) ^ t
	t += key[2]
	t = bits.RotateLeft32(t, 1) - t
	t = bits.RotateLeft32(t, 16) ^ (p[0] | t)
	p[1] ^= t
}

func multi2Pi4(p *[2]uint32, key []uint32) {
	t := p[1] + key[3]
	t = bits.RotateLeft32(t, 2) + t + 1
	p[0] ^= t
}

func multi2Setup(dataKey [2]uint32, systemKey [8]uint32) [8]uint32 {
	p := dataKey
	var userKey [8]uint32
	n := 0

	multi2Pi1(&p)
	multi2Pi2(&p, systemKey[0:])
	userKey[n] = p[0]
	n++
	multi2Pi3(&p, systemKey[0:])
	userKey[n] = p[1]
	n++
	multi2Pi4(&p, systemKey[0:])
	userKey[n] = p[0]
	n++
	multi2Pi1(&p)
	userKey[n] = p[1]
	n++
	multi2Pi2(&p, systemKey[4:])
	userKey[n] = p[0]
	n++
	multi2Pi3(&p, systemKey[4:])
	userKey[n] = p[1]
	n++
	multi2Pi4(&p, systemKey[4:])
	userKey[n] = p[0]
	n++
	multi2Pi1(&p)
	userKey[n] = p[1]

	return userKey
}

func multi2EncryptBlock(p *[2]uint32, rounds int, userKey *[8]uint32) {
	keyOffset := 0
	for round := 0; ; {
		multi2Pi1(p)
		round++
		if round == rounds {
			return
		}
		multi2Pi2(p, userKey[keyOffset:])
		round++
		if round == rounds {
			return
		}
		multi2Pi3(p, userKey[keyOffset:])
		round++
		if round == rounds {
			return
		}
		multi2Pi4(p, userKey[keyOffset:])
		round++
		if round == rounds {
			return
		}
		keyOffset ^= 4
	}
}

func multi2DecryptBlock(p *[2]uint32, rounds int, userKey *[8]uint32) {
	keyOffset := 4 * (((rounds - 1) >> 2) & 1)
	for round := rounds; ; {
		switch {
		case round == 0:
			return
		case round <= 4:
			for ; round > 0; round-- {
				switch round {
				case 4:
					multi2Pi4(p, userKey[keyOffset:])
				case 3:
					multi2Pi3(p, userKey[keyOffset:])
				case 2:
					multi2Pi2(p, userKey[keyOffset:])
				case 1:
					multi2Pi1(p)
				}
			}
		default:
			switch ((round - 1) % 4) + 1 {
			case 4:
				multi2Pi4(p, userKey[keyOffset:])
				round--
				fallthrough
			case 3:
				multi2Pi3(p, userKey[keyOffset:])
				round--
				fallthrough
			case 2:
				multi2Pi2(p, userKey[keyOffset:])
				round--
				fallthrough
			case 1:
				multi2Pi1(p)
				round--
			}
		}
		keyOffset ^= 4
	}
}

func (m *Multi2Cipher) Encrypt(data []byte) ([]byte, error) {
	if m.N != multi2Rounds {
		return nil, ErrInvalidKeySize
	}
	if len(data)%m.BlockSize() != 0 {
		return nil, ErrInvalidBlockSize
	}
	out := make([]byte, len(data))
	for offset := 0; offset < len(data); offset += m.BlockSize() {
		block := [2]uint32{
			binary.BigEndian.Uint32(data[offset : offset+4]),
			binary.BigEndian.Uint32(data[offset+4 : offset+8]),
		}
		multi2EncryptBlock(&block, m.N, &m.uk)
		binary.BigEndian.PutUint32(out[offset:offset+4], block[0])
		binary.BigEndian.PutUint32(out[offset+4:offset+8], block[1])
	}
	return out, nil
}

func (m *Multi2Cipher) Decrypt(data []byte) ([]byte, error) {
	if m.N != multi2Rounds {
		return nil, ErrInvalidKeySize
	}
	if len(data)%m.BlockSize() != 0 {
		return nil, ErrInvalidBlockSize
	}
	out := make([]byte, len(data))
	for offset := 0; offset < len(data); offset += m.BlockSize() {
		block := [2]uint32{
			binary.BigEndian.Uint32(data[offset : offset+4]),
			binary.BigEndian.Uint32(data[offset+4 : offset+8]),
		}
		multi2DecryptBlock(&block, m.N, &m.uk)
		binary.BigEndian.PutUint32(out[offset:offset+4], block[0])
		binary.BigEndian.PutUint32(out[offset+4:offset+8], block[1])
	}
	return out, nil
}
