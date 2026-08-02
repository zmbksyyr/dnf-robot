package nocache

import (
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"time"

	foundationnetwork "robot/internal/foundation/network"
)

const (
	packetOpcode = 0x1b6d
	packetSize   = 22

	ModeGameAndMonitor = 0
)

type Client struct {
	GameAddress string
	ServerGroup uint32
	Timeout     time.Duration
}

func NewClient(host string, gamePort, serverGroup int) (*Client, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		host = "127.0.0.1"
	}
	gameUDPPort, err := GameUDPPort(gamePort)
	if err != nil {
		return nil, err
	}
	if serverGroup < 0 || uint64(serverGroup) > math.MaxUint32 {
		return nil, fmt.Errorf("invalid server group %d", serverGroup)
	}
	return &Client{
		GameAddress: net.JoinHostPort(host, strconv.Itoa(gameUDPPort)),
		ServerGroup: uint32(serverGroup),
		Timeout:     time.Second,
	}, nil
}

func (c Client) Invalidate(uid uint32) error {
	if uid == 0 {
		return fmt.Errorf("invalid cache uid 0")
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = time.Second
	}

	packet := BuildPacket(uid, c.ServerGroup, ModeGameAndMonitor)
	if err := sendPacket("udp4", c.GameAddress, packet, timeout); err != nil {
		return fmt.Errorf("invalidate character cache through game at %s: %w", c.GameAddress, err)
	}
	return nil
}

func BuildPacket(uid, serverGroup, mode uint32) []byte {
	packet := make([]byte, packetSize)
	binary.LittleEndian.PutUint16(packet[0:2], packetOpcode)
	binary.LittleEndian.PutUint16(packet[2:4], packetSize)
	binary.LittleEndian.PutUint32(packet[0x0a:0x0e], uid)
	binary.LittleEndian.PutUint32(packet[0x0e:0x12], serverGroup)
	binary.LittleEndian.PutUint32(packet[0x12:0x16], mode)
	return packet
}

func GameUDPPort(gamePort int) (int, error) {
	const udpOffset = 1000
	if gamePort <= 0 || gamePort > 65535-udpOffset {
		return 0, fmt.Errorf("invalid game TCP port %d for UDP offset %d", gamePort, udpOffset)
	}
	return gamePort + udpOffset, nil
}

func sendPacket(network, address string, packet []byte, timeout time.Duration) error {
	if address == "" {
		return fmt.Errorf("empty address")
	}
	conn, err := net.DialTimeout(network, address, timeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	return foundationnetwork.WriteFull(conn, packet)
}
