package crypt

import (
	"encoding/binary"
)

func SwapUint16(value uint16) uint16 {
	return ((value & 0x00FF) << 8) | ((value & 0xFF00) >> 8)
}

func SwapUint32(value uint32) uint32 {
	return ((value & 0x000000FF) << 24) | ((value & 0x0000FF00) << 8) | ((value & 0x00FF0000) >> 8) | ((value & 0xFF000000) >> 24)
}

type md5State struct {
	count [2]uint32
	abcd  [4]uint32
	buf   [64]byte
}

var md5Pad = [64]byte{
	0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
}

func md5Init(pms *md5State) {
	pms.count[0] = 0
	pms.count[1] = 0
	pms.abcd[0] = 0x67452301
	pms.abcd[1] = 0xEFCDAB89
	pms.abcd[2] = 0x98BADCFE
	pms.abcd[3] = 0x10325476
}

func md5Process(pms *md5State, data []byte) {
	a := pms.abcd[0]
	b := pms.abcd[1]
	c := pms.abcd[2]
	d := pms.abcd[3]
	var X [16]uint32
	for i := 0; i < 16; i++ {
		X[i] = uint32(data[4*i]) | uint32(data[4*i+1])<<8 | uint32(data[4*i+2])<<16 | uint32(data[4*i+3])<<24
	}

	a = md5FF(a, b, c, d, X[0], 7, 0xd76aa478)
	d = md5FF(d, a, b, c, X[1], 12, 0xe8c7b756)
	c = md5FF(c, d, a, b, X[2], 17, 0x242070db)
	b = md5FF(b, c, d, a, X[3], 22, 0xc1bdceee)
	a = md5FF(a, b, c, d, X[4], 7, 0xf57c0faf)
	d = md5FF(d, a, b, c, X[5], 12, 0x4787c62a)
	c = md5FF(c, d, a, b, X[6], 17, 0xa8304613)
	b = md5FF(b, c, d, a, X[7], 22, 0xfd469501)
	a = md5FF(a, b, c, d, X[8], 7, 0x698098d8)
	d = md5FF(d, a, b, c, X[9], 12, 0x8b44f7af)
	c = md5FF(c, d, a, b, X[10], 17, 0xffff5bb1)
	b = md5FF(b, c, d, a, X[11], 22, 0x895cd7be)
	a = md5FF(a, b, c, d, X[12], 7, 0x6b901122)
	d = md5FF(d, a, b, c, X[13], 12, 0xfd987193)
	c = md5FF(c, d, a, b, X[14], 17, 0xa679438e)
	b = md5FF(b, c, d, a, X[15], 22, 0x49b40821)

	a = md5GG(a, b, c, d, X[1], 5, 0xf61e2562)
	d = md5GG(d, a, b, c, X[6], 9, 0xc040b340)
	c = md5GG(c, d, a, b, X[11], 14, 0x265e5a51)
	b = md5GG(b, c, d, a, X[0], 20, 0xe9b6c7aa)
	a = md5GG(a, b, c, d, X[5], 5, 0xd62f105d)
	d = md5GG(d, a, b, c, X[10], 9, 0x02441453)
	c = md5GG(c, d, a, b, X[15], 14, 0xd8a1e681)
	b = md5GG(b, c, d, a, X[4], 20, 0xe7d3fbc8)
	a = md5GG(a, b, c, d, X[9], 5, 0x21e1cde6)
	d = md5GG(d, a, b, c, X[14], 9, 0xc33707d6)
	c = md5GG(c, d, a, b, X[3], 14, 0xf4d50d87)
	b = md5GG(b, c, d, a, X[8], 20, 0x455a14ed)
	a = md5GG(a, b, c, d, X[13], 5, 0xa9e3e905)
	d = md5GG(d, a, b, c, X[2], 9, 0xfcefa3f8)
	c = md5GG(c, d, a, b, X[7], 14, 0x676f02d9)
	b = md5GG(b, c, d, a, X[12], 20, 0x8d2a4c8a)

	a = md5HH(a, b, c, d, X[5], 4, 0xfffa3942)
	d = md5HH(d, a, b, c, X[8], 11, 0x8771f681)
	c = md5HH(c, d, a, b, X[11], 16, 0x6d9d6122)
	b = md5HH(b, c, d, a, X[14], 23, 0xfde5380c)
	a = md5HH(a, b, c, d, X[1], 4, 0xa4beea44)
	d = md5HH(d, a, b, c, X[4], 11, 0x4bdecfa9)
	c = md5HH(c, d, a, b, X[7], 16, 0xf6bb4b60)
	b = md5HH(b, c, d, a, X[10], 23, 0xbebfbc70)
	a = md5HH(a, b, c, d, X[13], 4, 0x289b7ec6)
	d = md5HH(d, a, b, c, X[0], 11, 0xeaa127fa)
	c = md5HH(c, d, a, b, X[3], 16, 0xd4ef3085)
	b = md5HH(b, c, d, a, X[6], 23, 0x04881d05)
	a = md5HH(a, b, c, d, X[9], 4, 0xd9d4d039)
	d = md5HH(d, a, b, c, X[12], 11, 0xe6db99e5)
	c = md5HH(c, d, a, b, X[15], 16, 0x1fa27cf8)
	b = md5HH(b, c, d, a, X[2], 23, 0xc4ac5665)

	a = md5II(a, b, c, d, X[0], 6, 0xf4292244)
	d = md5II(d, a, b, c, X[7], 10, 0x432aff97)
	c = md5II(c, d, a, b, X[14], 15, 0xab9423a7)
	b = md5II(b, c, d, a, X[5], 21, 0xfc93a039)
	a = md5II(a, b, c, d, X[12], 6, 0x655b59c3)
	d = md5II(d, a, b, c, X[3], 10, 0x8f0ccc92)
	c = md5II(c, d, a, b, X[10], 15, 0xffeff47d)
	b = md5II(b, c, d, a, X[1], 21, 0x85845dd1)
	a = md5II(a, b, c, d, X[8], 6, 0x6fa87e4f)
	d = md5II(d, a, b, c, X[15], 10, 0xfe2ce6e0)
	c = md5II(c, d, a, b, X[6], 15, 0xa3014314)
	b = md5II(b, c, d, a, X[13], 21, 0x4e0811a1)
	a = md5II(a, b, c, d, X[4], 6, 0xf7537e82)
	d = md5II(d, a, b, c, X[11], 10, 0xbd3af235)
	c = md5II(c, d, a, b, X[2], 15, 0x2ad7d2bb)
	b = md5II(b, c, d, a, X[9], 21, 0xeb86d391)

	pms.abcd[0] += a
	pms.abcd[1] += b
	pms.abcd[2] += c
	pms.abcd[3] += d
}

func md5FF(a, b, c, d, x uint32, s int, ac uint32) uint32 {
	a += ((b & c) | (^b & d)) + x + ac
	a = (a << s) | (a >> (32 - s))
	a += b
	return a
}

func md5GG(a, b, c, d, x uint32, s int, ac uint32) uint32 {
	a += ((b & d) | (c & ^d)) + x + ac
	a = (a << s) | (a >> (32 - s))
	a += b
	return a
}

func md5HH(a, b, c, d, x uint32, s int, ac uint32) uint32 {
	a += (b ^ c ^ d) + x + ac
	a = (a << s) | (a >> (32 - s))
	a += b
	return a
}

func md5II(a, b, c, d, x uint32, s int, ac uint32) uint32 {
	a += (c ^ (b | ^d)) + x + ac
	a = (a << s) | (a >> (32 - s))
	a += b
	return a
}

func md5Append(pms *md5State, data []byte, nbytes int) {
	if nbytes <= 0 {
		return
	}
	v3 := pms.count[0]
	pms.count[1] += uint32(nbytes >> 29)
	pms.count[0] = uint32(8*nbytes) + v3
	if pms.count[0] < uint32(8*nbytes) {
		pms.count[1]++
	}
	v4 := v3 >> 3
	v7 := int(v4 & 0x3F)
	v5 := data
	v6 := nbytes
	if v7 != 0 {
		v8 := v7 + v6
		if v8 > 64 {
			v6 = 64 - v7
			v8 = 64
		}
		copy(pms.buf[v7:], data[:v6])
		if v8 > 63 {
			v5 = data[v6:]
			v6 = nbytes - v6
			md5Process(pms, pms.buf[:])
			for v6 > 63 {
				md5Process(pms, v5[:64])
				v6 -= 64
				v5 = v5[64:]
			}
			if v6 > 0 {
				copy(pms.buf[:], v5[:v6])
			}
		}
	} else {
		for v6 > 63 {
			md5Process(pms, v5[:64])
			v6 -= 64
			v5 = v5[64:]
		}
		if v6 > 0 {
			copy(pms.buf[:], v5[:v6])
		}
	}
}

func md5Finish(pms *md5State, digest []byte) {
	var data [8]byte
	for i := 0; i < 8; i++ {
		data[i] = byte(pms.count[i>>2] >> (8 * (i & 3)))
	}
	padLen := ((55 - int(pms.count[0]>>3)) & 0x3F) + 1
	md5Append(pms, md5Pad[:], padLen)
	md5Append(pms, data[:], 8)
	for i := 0; i < 16; i++ {
		digest[i] = byte(pms.abcd[i>>2] >> (8 * (i & 3)))
	}
}

func TenMd5(data []byte) []byte {
	var state md5State
	var digest [16]byte
	md5Init(&state)
	md5Append(&state, data, len(data))
	md5Finish(&state, digest[:])
	result := make([]byte, 16)
	copy(result, digest[:])
	return result
}

func GenKey(nType int, dwKeySeed uint32) []byte {
	byTmpRandBuf := make([]byte, 10)
	pbyTmpKey := make([]byte, 32)
	for idx := 0; idx < 2; idx++ {
		binary.LittleEndian.PutUint32(byTmpRandBuf[0:4], dwKeySeed)
		v5 := idx + nType
		binary.LittleEndian.PutUint32(byTmpRandBuf[4:8], uint32(v5))
		digest := TenMd5(byTmpRandBuf[:8])
		copy(pbyTmpKey[idx*16:(idx+1)*16], digest)
	}
	return pbyTmpKey
}

func GeneNew(key []byte, isEnc bool, buf []byte, bufSize int) {
	if bufSize == 0 {
		return
	}
	n1 := 0
	if isEnc {
		for n1 < bufSize {
			n2 := n1 & 7
			c1 := byte(n1) ^ buf[n1]
			buf[n1] = c1
			buf[n1] = key[n2] ^ c1
			n1++
		}
	} else {
		for n1 < bufSize {
			c2 := byte(n1) ^ key[n1&7] ^ buf[n1]
			n1++
			buf[n1-1] = c2
		}
	}
}
