package service

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

const monitorMegaphoneAddress = "127.0.0.1:30303"

func sendMonitorMegaphone(msg, name string, senderID uint16) error {
	msgBytes := []byte(msg)
	if len(msgBytes) > 255 {
		msgBytes = msgBytes[:255]
	}
	nameBytes := windows1252StringBytes(name)
	if len(nameBytes) == 0 {
		nameBytes = []byte("Robot")
	}
	if len(nameBytes) > 0x1e {
		nameBytes = nameBytes[:0x1e]
	}

	size := 0x2e + len(msgBytes)
	packet := make([]byte, size)
	binary.LittleEndian.PutUint16(packet[0:2], 0x546)
	binary.LittleEndian.PutUint16(packet[2:4], uint16(size))
	packet[0x0b] = 11
	binary.LittleEndian.PutUint16(packet[0x0c:0x0e], senderID)
	packet[0x0e] = 15
	copy(packet[0x0f:0x2d], nameBytes)
	packet[0x2d] = byte(len(msgBytes))
	copy(packet[0x2e:], msgBytes)

	conn, err := net.DialTimeout("tcp", monitorMegaphoneAddress, 3*time.Second)
	if err != nil {
		return fmt.Errorf("connect monitor megaphone: %w", err)
	}
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write(packet); err != nil {
		return fmt.Errorf("send monitor megaphone: %w", err)
	}
	return nil
}
