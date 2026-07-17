package dnf

import (
	"bytes"
	"encoding/binary"
	"net"
	"robot/internal/protocol/dnf/crypt"
	"testing"
	"time"
)

func TestBuildFinishLoadingPayload(t *testing.T) {
	got := buildFinishLoadingPayload(0x01020304, 0xa0b0c0d0)
	want := []byte{0x04, 0x03, 0x02, 0x01, 0xd0, 0xc0, 0xb0, 0xa0}
	if !bytes.Equal(got, want) {
		t.Fatalf("payload = %x, want %x", got, want)
	}
}

func TestDungeonLoadingNotificationSendsFinishLoading(t *testing.T) {
	for _, packetType := range []uint16{28, 29} {
		t.Run(NotiPacketNames[int(packetType)], func(t *testing.T) {
			cipher := crypt.NewDNFCipher()
			if err := cipher.Initialize(make([]byte, 334)); err != nil {
				t.Fatalf("initialize cipher: %v", err)
			}
			robotConn, peerConn := net.Pipe()
			defer robotConn.Close()
			defer peerConn.Close()

			vo := &RobotVo{State: StateRun, Cipher: cipher, Conn: robotConn, PacketID: 7}
			packet := make([]byte, 32)
			binary.LittleEndian.PutUint16(packet[1:3], packetType)
			binary.LittleEndian.PutUint32(packet[3:7], uint32(len(packet)))
			copy(packet[15:], []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17})

			gotCh := make(chan []byte, 1)
			go func() {
				buf := make([]byte, 64)
				n, _ := peerConn.Read(buf)
				gotCh <- append([]byte(nil), buf[:n]...)
			}()

			vo.parsePacket(packet)
			select {
			case got := <-gotCh:
				assertFinishLoadingPacket(t, cipher, got, 7)
			case <-time.After(time.Second):
				t.Fatal("finish-loading packet was not sent")
			}
			if vo.PacketID != 8 {
				t.Fatalf("next packet id = %d, want 8", vo.PacketID)
			}
		})
	}
}

func TestShortDungeonLoadingNotificationIsIgnored(t *testing.T) {
	vo := &RobotVo{State: StateRun, Cipher: crypt.NewDNFCipher(), PacketID: 7}
	packet := make([]byte, 7)
	binary.LittleEndian.PutUint16(packet[1:3], 28)
	binary.LittleEndian.PutUint32(packet[3:7], uint32(len(packet)))
	vo.parsePacket(packet)
	if vo.PacketID != 7 {
		t.Fatalf("short packet advanced packet id to %d", vo.PacketID)
	}
}

func assertFinishLoadingPacket(t *testing.T, cipher *crypt.DNFCipher, packet []byte, index uint16) {
	t.Helper()
	if len(packet) != 21 || packet[0] != 1 || binary.LittleEndian.Uint16(packet[1:3]) != 40 {
		t.Fatalf("packet header = %x", packet)
	}
	if got := binary.LittleEndian.Uint16(packet[11:13]); got != index {
		t.Fatalf("packet index = %d, want %d", got, index)
	}
	payload, err := cipher.Decrypt(40, packet[13:])
	if err != nil {
		t.Fatalf("decrypt payload: %v", err)
	}
	if !bytes.Equal(payload, make([]byte, 8)) {
		t.Fatalf("payload = %x, want zero checksums", payload)
	}
	crcInput := append(append([]byte(nil), packet[11:13]...), payload...)
	crc := cipher.CRC32(0, crcInput)
	hash := make([]byte, 4)
	binary.LittleEndian.PutUint32(hash, crc)
	hash[0] ^= hash[1] ^ hash[2] ^ hash[3] ^ 0x18
	if binary.LittleEndian.Uint32(packet[7:11]) != binary.LittleEndian.Uint32(hash) {
		t.Fatalf("packet CRC = %x, want %x", packet[7:11], hash)
	}
}
