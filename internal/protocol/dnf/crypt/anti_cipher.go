package crypt

import (
	"crypto/cipher"
	"crypto/des"
	"crypto/rc4"
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"

	"github.com/deatil/go-cryptobin/cipher/idea"
	"github.com/deatil/go-cryptobin/cipher/mars"
	"github.com/deatil/go-cryptobin/cipher/rc2"
	"github.com/deatil/go-cryptobin/cipher/rc5"
	"github.com/deatil/go-cryptobin/cipher/square"
	"golang.org/x/crypto/twofish"
)

var ErrUnsupportedAntiCipher = errors.New("unsupported anti cipher")

// The anti channel has its own algorithm numbering and key conventions. These
// are not interchangeable with the fourteen regular packet ciphers.
func decryptAntiPayload(typ uint32, seed uint32, data []byte) error {
	key := GenKey(int(typ), seed)
	var block cipher.Block
	var err error
	boxTail := false
	boxKeySize := 16
	ideaWords := false
	switch typ {
	case 1:
		blockKey := append([]byte(nil), key[:16]...)
		swapAntiWords(blockKey)
		block, err = idea.NewCipher(blockKey)
		boxTail, ideaWords = true, true
	case 2:
		n := len(data) / 8 * 8
		decryptAntiNewDES(key, data[:n])
		decryptAntiBoxTail(key[:15], data[n:])
		return nil
	case 3:
		block, err = des.NewCipher(key[:8])
		boxTail = true
		boxKeySize = 8
	case 4:
		cast := NewCast5Cipher()
		if err = cast.SetKey(key[:16]); err != nil {
			return err
		}
		var plain []byte
		n := len(data) / 8 * 8
		plain, err = cast.Decrypt(data[:n])
		copy(data, plain)
		if err == nil {
			decryptAntiBoxTail(key[:16], data[n:])
		}
		return err
	case 5:
		n := len(data) / 8 * 8
		decryptAntiTean(key, data[:n])
		decryptAntiBoxTail(key[:16], data[n:])
		return nil
	case 6:
		// This variant uses the full 1024-bit expanded RC2 schedule.
		block, err = rc2.NewCipher(key[:16], 1024)
	case 7, 8, 11:
		n := map[uint32]int{7: 32, 8: 24, 11: 16}[typ]
		blockKey := append([]byte(nil), key[:n]...)
		swapAntiWords(blockKey)
		block, err = twofish.NewCipher(blockKey)
	case 9:
		n := len(data) / 16 * 16
		decryptAntiMMB(key, data[:n])
		decryptAntiSeattos(key, data[n:])
		return nil
	case 10:
		decryptAntiNSEA(key, data)
		return nil
	case 12:
		block, err = square.NewCipher(key[:16])
	case 13:
		block, err = mars.NewCipher(key[:16])
	case 14:
		var stream *rc4.Cipher
		stream, err = rc4.NewCipher(key[:16])
		if err == nil {
			stream.XORKeyStream(data, data)
		}
		return err
	case 15:
		decryptAntiSeattos(key, data)
		return nil
	case 16:
		decryptAntiGene(key, data)
		return nil
	case 17:
		decryptAntiRC6(key, data[:len(data)/16*16])
		decryptAntiSeattos(key, data[len(data)/16*16:])
		return nil
	case 18:
		block, err = rc5.NewCipher(key[:16], 32, 12)
		if err != nil {
			return err
		}
		// The wire variant advances 16 bytes after each 8-byte RC5 block;
		// the next eight bytes in each complete stride are left untouched.
		n := len(data) / 16 * 16
		for i := 0; i < n; i += 16 {
			block.Decrypt(data[i:i+8], data[i:i+8])
		}
		decryptAntiSeattos(key, data[n:])
		return nil
	case 19:
		GeneNew(key, false, data, len(data))
		return nil
	default:
		return fmt.Errorf("%w: type=%d", ErrUnsupportedAntiCipher, typ)
	}
	if err != nil {
		return err
	}
	n := len(data) / block.BlockSize() * block.BlockSize()
	for offset := 0; offset < n; offset += block.BlockSize() {
		part := data[offset : offset+block.BlockSize()]
		if ideaWords {
			swapAntiWords(part)
		}
		block.Decrypt(part, part)
	}
	if boxTail {
		decryptAntiBoxTail(key[:boxKeySize], data[n:])
	} else {
		decryptAntiSeattos(key, data[n:])
	}
	return nil
}

// Gene builds a keyed permutation with the Windows CRT linear congruential
// sequence. The wire substitution is conjugated by an eight-byte salt;
// decryption applies the inverse permutation before removing the byte index.
func decryptAntiGene(key, data []byte) {
	mask := [8]byte{0x8a, 0xe6, 0x9b, 0xf3, 0xc1, 0x7d, 0x40, 0x25}
	for i := range mask {
		mask[i] ^= key[i]
	}
	state := binary.BigEndian.Uint32(mask[:4]) ^ binary.BigEndian.Uint32(mask[4:])
	var forward, inverse [256]byte
	for i := range forward {
		forward[i] = byte(i)
	}
	for i := range forward {
		state = state*214013 + 2531011
		j := byte(state >> 16)
		forward[i], forward[j] = forward[j], forward[i]
	}
	for i, value := range forward {
		inverse[value] = byte(i)
	}
	for i, value := range data {
		m := mask[i&7]
		data[i] = inverse[value^m] ^ m ^ byte(i)
	}
}

func swapAntiWords(data []byte) {
	for i := 0; i+1 < len(data); i += 2 {
		data[i], data[i+1] = data[i+1], data[i]
	}
}

// Seattos is also the residual-byte transform used by RC2, Twofish, RC5 and
// RC6. Its byte counter restarts at the beginning of the residual slice.
func decryptAntiSeattos(key, data []byte) {
	salt := [...]byte{0x37, 0x92, 0x44, 0x68, 0xa5, 0x3d, 0xcc, 0x7f,
		0xbb, 0x0f, 0xd9, 0x88, 0xee, 0x9a, 0xe9, 0x5a}
	for i, value := range data {
		mask := salt[i&15]
		data[i] = ((value ^ mask ^ key[i&7]) - mask) ^ mask ^ key[i&7]
	}
}

// Tean uses addition of the key term and (word XOR sum), rather than the
// XOR of sums used by standard XTEA. Words on this channel are little endian.
func decryptAntiTean(key, data []byte) {
	var k [4]uint32
	for i := range k {
		k[i] = binary.LittleEndian.Uint32(key[i*4:])
	}
	for i := 0; i < len(data); i += 8 {
		a := binary.LittleEndian.Uint32(data[i:])
		b := binary.LittleEndian.Uint32(data[i+4:])
		sum := uint32(0xc6ef3720)
		for round := 0; round < 32; round++ {
			b -= ((a<<4 ^ a>>5) + (a ^ sum)) + k[(sum>>11)&3]
			sum -= 0x9e3779b9
			a -= ((b<<4 ^ b>>5) + (b ^ sum)) + k[sum&3]
		}
		binary.LittleEndian.PutUint32(data[i:], a)
		binary.LittleEndian.PutUint32(data[i+4:], b)
	}
}

func decryptAntiRC6(key, data []byte) {
	var schedule [44]uint32
	var words [8]uint32
	for i := range words {
		words[i] = uint32(key[i]) // One key byte per word in this anti variant.
	}
	schedule[0] = 0xb7e15163
	for i := 1; i < len(schedule); i++ {
		schedule[i] = schedule[i-1] + 0x9e3779b9
	}
	var a, b uint32
	for step := 0; step < 132; step++ {
		i, j := step%44, step%8
		a = bits.RotateLeft32(schedule[i]+a+b, 3)
		schedule[i] = a
		b = bits.RotateLeft32(words[j]+a+b, int((a+b)&31))
		words[j] = b
	}
	for i := 0; i < len(data); i += 16 {
		a := binary.LittleEndian.Uint32(data[i:]) - schedule[42]
		b := binary.LittleEndian.Uint32(data[i+4:])
		c := binary.LittleEndian.Uint32(data[i+8:]) - schedule[43]
		d := binary.LittleEndian.Uint32(data[i+12:])
		for round := 20; round > 0; round-- {
			a, b, c, d = d, a, b, c
			t := bits.RotateLeft32(b*(2*b+1), 5)
			u := bits.RotateLeft32(d*(2*d+1), 5)
			c = bits.RotateLeft32(c-schedule[2*round+1], -int(t&31)) ^ u
			a = bits.RotateLeft32(a-schedule[2*round], -int(u&31)) ^ t
		}
		binary.LittleEndian.PutUint32(data[i:], a)
		binary.LittleEndian.PutUint32(data[i+4:], b-schedule[0])
		binary.LittleEndian.PutUint32(data[i+8:], c)
		binary.LittleEndian.PutUint32(data[i+12:], d-schedule[1])
	}
}
