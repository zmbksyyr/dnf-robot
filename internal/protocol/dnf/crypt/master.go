package crypt

import (
	"encoding/binary"
)

type DNFCipher struct {
	handles    [14]BlockCipher
	keys       []byte
	keySize    int
	polynomial uint32
	haveTable  bool
	table      [256]uint32
}

func NewDNFCipher() *DNFCipher {
	c := &DNFCipher{
		polynomial: 1303941417,
	}
	c.handles[0] = NewShiftCipher()
	c.handles[1] = NewRijndaelCipher()
	c.handles[2] = NewBlowFishCipher()
	c.handles[3] = NewRc6Cipher()
	c.handles[4] = NewTwoFishCipher()
	c.handles[5] = NewTeaCipher()
	c.handles[6] = NewKasumiCipher()
	c.handles[7] = NewXteaCipher()
	c.handles[8] = NewNoekeonCipher()
	c.handles[9] = NewKhazadCipher()
	c.handles[10] = NewCast5Cipher()
	c.handles[11] = NewSkipjackCipher()
	c.handles[12] = NewMulti2Cipher()
	c.handles[13] = NewAnubisCipher()

	for i := 0; i < 14; i++ {
		if c.handles[i] != nil {
			c.keySize += c.handles[i].KeySize()
		}
	}
	return c
}

func (c *DNFCipher) Initialize(key []byte) error {
	// [C++->Go] C++ always passes 334 bytes, distributed across 14 ciphers
	// The Go ciphers may have a different total keySize; we accept up to that many bytes
	keyLen := len(key)
	if keyLen > c.keySize {
		keyLen = c.keySize
	}
	c.keys = make([]byte, keyLen)
	copy(c.keys, key[:keyLen])
	pBuffer := key[:keyLen]
	for i := 0; i < 14; i++ {
		ks := c.handles[i].KeySize()
		if ks > len(pBuffer) {
			break
		}
		if err := c.handles[i].SetKey(pBuffer[:ks]); err != nil {
			return err
		}
		pBuffer = pBuffer[ks:]
	}
	return nil
}

func (c *DNFCipher) KeySize() int { return c.keySize }

func (c *DNFCipher) Encrypt(packetType uint16, data []byte) ([]byte, error) {
	return c.handles[packetType%14].Encrypt(data)
}

func (c *DNFCipher) Decrypt(packetType uint16, data []byte) ([]byte, error) {
	return c.handles[packetType%14].Decrypt(data)
}

func (c *DNFCipher) DecryptLogin(data []byte) ([]byte, error) {
	v5 := uint32(71646901)
	out := make([]byte, len(data))
	copy(out, data)
	dest := byte((v5 >> 8) & 7)
	for i := 0; i < len(data); i++ {
		b := out[i]
		b = (b << dest) | (b >> (8 - dest))
		b ^= byte(v5)
		out[i] = b
	}
	return out, nil
}

type AntiHeader struct {
	PacketSize   uint16
	ProtocolType uint16
	DataType     byte
}

type AntiBody struct {
	KeySeed    uint32
	CryptoType uint32
	CRC32      uint32
}

type UnAntiHeader struct {
	UsrIdCRC   uint32
	CustomData uint32
	PadLen     byte
	PadData    [256]byte
}

func (c *DNFCipher) DecryptAnti(data []byte) ([]byte, error) {
	if len(data) <= 17 {
		return nil, ErrInvalidBlockSize
	}
	pHeader := AntiHeader{
		PacketSize:   binary.BigEndian.Uint16(data[0:2]),
		ProtocolType: binary.BigEndian.Uint16(data[2:4]),
		DataType:     data[4],
	}
	pBody := AntiBody{
		KeySeed:    binary.BigEndian.Uint32(data[5:9]),
		CryptoType: binary.BigEndian.Uint32(data[9:13]),
		CRC32:      binary.BigEndian.Uint32(data[13:17]),
	}

	packetSize := pHeader.PacketSize
	if int(packetSize)-17 <= 0 {
		return nil, ErrInvalidBlockSize
	}
	if int(packetSize) > len(data) {
		return nil, ErrInvalidBlockSize
	}
	antiDataSize := int(packetSize) - 17
	antiDataBuf := make([]byte, antiDataSize+4096)
	copy(antiDataBuf, data[17:])

	protocolType := pHeader.ProtocolType
	nType := pBody.CryptoType
	dwKeySeed := pBody.KeySeed

	if protocolType != 17 || (nType != 18 && nType != 19) {
		return nil, ErrInvalidBlockSize
	}

	pbyKey := GenKey(int(nType), dwKeySeed)
	GeneNew(pbyKey, false, antiDataBuf, antiDataSize)

	offset := 4 + 4 + 1 + int(antiDataBuf[8])
	if offset >= antiDataSize {
		return nil, ErrInvalidBlockSize
	}
	if antiDataBuf[offset] == 0 {
		if antiDataSize-offset < 15 {
			return nil, ErrInvalidBlockSize
		}
	} else if antiDataBuf[offset] == 1 {
		if antiDataSize-offset < 13 {
			return nil, ErrInvalidBlockSize
		}
	}

	sizeCheck := binary.LittleEndian.Uint32(antiDataBuf[offset+3 : offset+7])
	if uint32(antiDataSize-offset) != sizeCheck {
		return nil, ErrInvalidBlockSize
	}

	result := make([]byte, antiDataSize-offset)
	copy(result, antiDataBuf[offset:])
	return result, nil
}

func (c *DNFCipher) GetTotalKeyLength() int {
	return c.keySize
}

func (c *DNFCipher) GetOriginalKey() []byte {
	out := make([]byte, len(c.keys))
	copy(out, c.keys)
	return out
}

func (c *DNFCipher) CRC32(crc uint32, data []byte) uint32 {
	if !c.haveTable {
		c.makeCRCTable()
	}
	crc = ^crc
	for _, b := range data {
		crc = (crc >> 8) ^ c.table[(crc^uint32(b))&0xFF]
	}
	return ^crc
}

func (c *DNFCipher) makeCRCTable() {
	c.haveTable = true
	for i := 0; i < 256; i++ {
		c.table[i] = uint32(i)
		for j := 0; j < 8; j++ {
			if c.table[i]&1 != 0 {
				c.table[i] = (c.table[i] >> 1) ^ c.polynomial
			} else {
				c.table[i] >>= 1
			}
		}
	}
}

func (c *DNFCipher) MakeChecksumTo1Byte(data []byte) {
	data[0] ^= data[2] ^ data[1] ^ data[3] ^ 0x18
}
