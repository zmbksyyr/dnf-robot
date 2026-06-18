package dnf

import (
	"encoding/binary"

	"robot/internal/dnf/crypt"
)

const dnfPacketRecvHeaderSize = 15

type DNFPacketRecvHeader struct {
	RecvFlag  uint8
	RecvType  uint16
	RecvSize  uint32
	RecvCRC32 uint32
	RecvHash  uint32
}

func ParsePacket(data []byte, cipher *crypt.DNFCipher) (*DNFPacketRecvHeader, []byte, error) {
	if len(data) < dnfPacketRecvHeaderSize {
		return nil, nil, crypt.ErrInvalidBlockSize
	}

	header := &DNFPacketRecvHeader{
		RecvFlag:  data[0],
		RecvType:  binary.LittleEndian.Uint16(data[1:3]),
		RecvSize:  binary.LittleEndian.Uint32(data[3:7]),
		RecvCRC32: binary.LittleEndian.Uint32(data[7:11]),
		RecvHash:  binary.LittleEndian.Uint32(data[11:15]),
	}

	if len(data) <= dnfPacketRecvHeaderSize {
		return header, nil, nil
	}

	bodySize := len(data) - dnfPacketRecvHeaderSize
	body := make([]byte, bodySize)
	copy(body, data[dnfPacketRecvHeaderSize:])

	var decrypted []byte
	var err error

	packetType := binary.LittleEndian.Uint16(data[1:3])
	if packetType == 1 {
		decrypted, err = cipher.DecryptLogin(body)
	} else {
		decrypted, err = cipher.Decrypt(packetType, body)
	}

	if err != nil {
		return header, nil, err
	}

	return header, decrypted, nil
}

func ParsePacketAnti(data []byte, cipher *crypt.DNFCipher) (*DNFPacketRecvHeader, []byte, error) {
	if len(data) < dnfPacketRecvHeaderSize {
		return nil, nil, crypt.ErrInvalidBlockSize
	}

	header := &DNFPacketRecvHeader{
		RecvFlag:  data[0],
		RecvType:  binary.LittleEndian.Uint16(data[1:3]),
		RecvSize:  binary.LittleEndian.Uint32(data[3:7]),
		RecvCRC32: binary.LittleEndian.Uint32(data[7:11]),
		RecvHash:  binary.LittleEndian.Uint32(data[11:15]),
	}

	if len(data) <= dnfPacketRecvHeaderSize {
		return header, nil, nil
	}

	decrypted, err := cipher.DecryptAnti(data[dnfPacketRecvHeaderSize:])
	if err != nil {
		return header, nil, err
	}

	return header, decrypted, nil
}
