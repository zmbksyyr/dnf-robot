package crypt

import "errors"

const (
	ECB = iota
	CBC
	CFB
)

var (
	ErrNotInitialized   = errors.New("cipher not initialized")
	ErrInvalidKeySize   = errors.New("invalid key size")
	ErrInvalidBlockSize = errors.New("data not multiple of block size")
	ErrOutBufTooSmall   = errors.New("output buffer too small")
)

type BlockCipher interface {
	Encrypt(data []byte) ([]byte, error)
	Decrypt(data []byte) ([]byte, error)
	SetKey(key []byte) error
	KeySize() int
	BlockSize() int
}

func loadU32BE(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func loadU32LE(b []byte) uint32 {
	return uint32(b[3])<<24 | uint32(b[2])<<16 | uint32(b[1])<<8 | uint32(b[0])
}

func storeU32BE(b []byte, v uint32) {
	b[0] = byte(v >> 24)
	b[1] = byte(v >> 16)
	b[2] = byte(v >> 8)
	b[3] = byte(v)
}

func storeU32LE(b []byte, v uint32) {
	b[3] = byte(v >> 24)
	b[2] = byte(v >> 16)
	b[1] = byte(v >> 8)
	b[0] = byte(v)
}

func rol32(v uint32, n int) uint32 {
	n &= 31
	if n == 0 {
		return v
	}
	return (v << n) | (v >> (32 - n))
}

func ror32(v uint32, n int) uint32 {
	n &= 31
	if n == 0 {
		return v
	}
	return (v >> n) | (v << (32 - n))
}

func loadU64BE(b []byte) uint64 {
	return uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 |
		uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7])
}

func storeU64BE(b []byte, v uint64) {
	b[0] = byte(v >> 56)
	b[1] = byte(v >> 48)
	b[2] = byte(v >> 40)
	b[3] = byte(v >> 32)
	b[4] = byte(v >> 24)
	b[5] = byte(v >> 16)
	b[6] = byte(v >> 8)
	b[7] = byte(v)
}
