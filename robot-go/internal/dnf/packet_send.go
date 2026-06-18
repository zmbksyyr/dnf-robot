package dnf

import (
	"encoding/binary"

	"robot/internal/dnf/crypt"
)

const dnfPacketHeaderSize = 13

type DNFPacketSendResult struct {
	HeaderSize int
	TotalSize  int
}

func BuildPacket(packetType uint16, data []byte, cipher *crypt.DNFCipher) ([]byte, error) {
	outSize := dnfPacketHeaderSize + len(data)
	outBuf := make([]byte, outSize)

	tmp := make([]byte, outSize)

	binary.LittleEndian.PutUint16(tmp[11:13], 0)
	if len(data) > 0 {
		copy(tmp[13:], data)
	}

	crc := cipher.CRC32(0, tmp[11:outSize])
	crcBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(crcBytes, crc)
	cipher.MakeChecksumTo1Byte(crcBytes)
	hash := binary.LittleEndian.Uint32(crcBytes)

	outBuf[0] = 1
	binary.LittleEndian.PutUint16(outBuf[1:3], packetType)
	binary.LittleEndian.PutUint32(outBuf[3:7], uint32(outSize))
	binary.LittleEndian.PutUint32(outBuf[7:11], hash)
	binary.LittleEndian.PutUint16(outBuf[11:13], 0)

	copy(tmp[0:13], outBuf[0:13])

	encrypted, err := cipher.Encrypt(packetType, tmp[13:outSize])
	if err != nil {
		return nil, err
	}

	result := make([]byte, 13+len(encrypted))
	copy(result[0:13], outBuf[0:13])
	copy(result[13:], encrypted)

	return result, nil
}

func BuildPacketWithIndex(packetType uint16, index uint16, data []byte, cipher *crypt.DNFCipher) ([]byte, error) {
	outSize := dnfPacketHeaderSize + len(data)
	outBuf := make([]byte, outSize)

	tmp := make([]byte, outSize)

	binary.LittleEndian.PutUint16(tmp[11:13], index)
	if len(data) > 0 {
		copy(tmp[13:], data)
	}

	crc := cipher.CRC32(0, tmp[11:outSize])
	crcBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(crcBytes, crc)
	cipher.MakeChecksumTo1Byte(crcBytes)
	hash := binary.LittleEndian.Uint32(crcBytes)

	outBuf[0] = 1
	binary.LittleEndian.PutUint16(outBuf[1:3], packetType)
	binary.LittleEndian.PutUint32(outBuf[3:7], uint32(outSize))
	binary.LittleEndian.PutUint32(outBuf[7:11], hash)
	binary.LittleEndian.PutUint16(outBuf[11:13], index)

	copy(tmp[0:13], outBuf[0:13])

	encrypted, err := cipher.Encrypt(packetType, tmp[13:outSize])
	if err != nil {
		return nil, err
	}

	result := make([]byte, 13+len(encrypted))
	copy(result[0:13], outBuf[0:13])
	copy(result[13:], encrypted)

	return result, nil
}
