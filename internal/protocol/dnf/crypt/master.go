package crypt

import (
	"encoding/binary"
)

type DNFCipher struct {
	handles     [14]BlockCipher
	keySize     int
	initialized bool
}

const dnfCRCPolynomial uint32 = 1303941417

var dnfCRCTable = buildDNFCRCTable()

func NewDNFCipher() *DNFCipher {
	c := &DNFCipher{}
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
	if c == nil || len(key) != c.keySize {
		return ErrInvalidKeySize
	}
	pBuffer := key
	for i := 0; i < 14; i++ {
		if c.handles[i] == nil {
			c.initialized = false
			return ErrNotInitialized
		}
		ks := c.handles[i].KeySize()
		if err := c.handles[i].SetKey(pBuffer[:ks]); err != nil {
			c.initialized = false
			return err
		}
		pBuffer = pBuffer[ks:]
	}
	c.initialized = true
	return nil
}

func (c *DNFCipher) KeySize() int { return c.keySize }

func (c *DNFCipher) Encrypt(packetType uint16, data []byte) ([]byte, error) {
	if c == nil || !c.initialized {
		return nil, ErrNotInitialized
	}
	return c.handles[packetType%14].Encrypt(data)
}

func (c *DNFCipher) Decrypt(packetType uint16, data []byte) ([]byte, error) {
	if c == nil || !c.initialized {
		return nil, ErrNotInitialized
	}
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

const maxAntiCryptoType = 19

func (c *DNFCipher) DecryptAnti(data []byte) ([]byte, error) {
	if len(data) <= 17 {
		return nil, ErrInvalidBlockSize
	}
	packetSize, keySeed, cryptoType, ok := antiEnvelopeHeader(data)
	if !ok {
		return nil, ErrInvalidBlockSize
	}

	antiDataSize := packetSize - 17
	if antiDataSize < 9 {
		return nil, ErrInvalidBlockSize
	}
	antiDataBuf := make([]byte, antiDataSize)
	copy(antiDataBuf, data[17:packetSize])

	pbyKey := GenKey(int(cryptoType), keySeed)
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
	} else if antiDataSize-offset < 7 {
		return nil, ErrInvalidBlockSize
	}

	sizeCheck := binary.LittleEndian.Uint32(antiDataBuf[offset+3 : offset+7])
	if uint32(antiDataSize-offset) != sizeCheck {
		return nil, ErrInvalidBlockSize
	}

	return antiDataBuf[offset:], nil
}

func antiEnvelopeHeader(data []byte) (packetSize int, keySeed, cryptoType uint32, ok bool) {
	// DPROTO deployments exist with both network-order and host-order envelope
	// fields. Validate the complete header before accepting either representation.
	for _, order := range []binary.ByteOrder{binary.BigEndian, binary.LittleEndian} {
		size := int(order.Uint16(data[0:2]))
		protocolType := order.Uint16(data[2:4])
		candidateCryptoType := order.Uint32(data[9:13])
		if size <= 17 || size > len(data) || protocolType != 17 || candidateCryptoType > maxAntiCryptoType {
			continue
		}
		return size, order.Uint32(data[5:9]), candidateCryptoType, true
	}
	return 0, 0, 0, false
}

func (c *DNFCipher) GetTotalKeyLength() int {
	return c.keySize
}

func (c *DNFCipher) CRC32(crc uint32, data []byte) uint32 {
	crc = ^crc
	for _, b := range data {
		crc = (crc >> 8) ^ dnfCRCTable[(crc^uint32(b))&0xFF]
	}
	return ^crc
}

func buildDNFCRCTable() [256]uint32 {
	var table [256]uint32
	for i := 0; i < 256; i++ {
		table[i] = uint32(i)
		for j := 0; j < 8; j++ {
			if table[i]&1 != 0 {
				table[i] = (table[i] >> 1) ^ dnfCRCPolynomial
			} else {
				table[i] >>= 1
			}
		}
	}
	return table
}

func (c *DNFCipher) MakeChecksumTo1Byte(data []byte) {
	data[0] ^= data[2] ^ data[1] ^ data[3] ^ 0x18
}
