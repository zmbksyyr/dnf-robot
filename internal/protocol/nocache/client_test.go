package nocache

import (
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func TestBuildPacket(t *testing.T) {
	packet := BuildPacket(17000001, 3, ModeGameAndMonitor)
	if len(packet) != packetSize {
		t.Fatalf("packet size = %d, want %d", len(packet), packetSize)
	}
	if got := binary.LittleEndian.Uint16(packet[0:2]); got != packetOpcode {
		t.Fatalf("opcode = %#x, want %#x", got, packetOpcode)
	}
	if got := binary.LittleEndian.Uint16(packet[2:4]); got != packetSize {
		t.Fatalf("header size = %d, want %d", got, packetSize)
	}
	if got := binary.LittleEndian.Uint32(packet[0x0a:0x0e]); got != 17000001 {
		t.Fatalf("uid = %d, want 17000001", got)
	}
	if got := binary.LittleEndian.Uint32(packet[0x0e:0x12]); got != 3 {
		t.Fatalf("server group = %d, want 3", got)
	}
	if got := binary.LittleEndian.Uint32(packet[0x12:0x16]); got != ModeGameAndMonitor {
		t.Fatalf("mode = %d, want %d", got, ModeGameAndMonitor)
	}
}

func TestGameUDPPort(t *testing.T) {
	port, err := GameUDPPort(10011)
	if err != nil || port != 11011 {
		t.Fatalf("GameUDPPort(10011) = %d, %v; want 11011, nil", port, err)
	}
	if _, err := GameUDPPort(65000); err == nil {
		t.Fatal("GameUDPPort accepted a TCP port whose UDP offset overflows")
	}
}

func TestNewClientDerivesNativeEndpoints(t *testing.T) {
	client, err := NewClient("192.168.200.131", 10011, 3)
	if err != nil {
		t.Fatal(err)
	}
	if client.GameAddress != "192.168.200.131:11011" || client.ServerGroup != 3 {
		t.Fatalf("endpoint=%s server_group=%d", client.GameAddress, client.ServerGroup)
	}
}

func TestClientSendsCombinedInvalidationThroughGame(t *testing.T) {
	game, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer game.Close()
	client := Client{
		GameAddress: game.LocalAddr().String(),
		ServerGroup: 3,
		Timeout:     time.Second,
	}
	if err := client.Invalidate(17000001); err != nil {
		t.Fatal(err)
	}

	_ = game.SetReadDeadline(time.Now().Add(2 * time.Second))
	gamePacket := make([]byte, packetSize)
	n, _, err := game.ReadFromUDP(gamePacket)
	if err != nil {
		t.Fatal(err)
	}
	if n != packetSize || binary.LittleEndian.Uint32(gamePacket[0x0e:0x12]) != 3 || binary.LittleEndian.Uint32(gamePacket[0x12:0x16]) != ModeGameAndMonitor {
		t.Fatalf("unexpected game packet size=%d group=%d mode=%d", n, binary.LittleEndian.Uint32(gamePacket[0x0e:0x12]), binary.LittleEndian.Uint32(gamePacket[0x12:0x16]))
	}
}
