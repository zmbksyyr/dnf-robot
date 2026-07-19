package monitor

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"robot/internal/foundation/charset"
)

const (
	defaultAddress = "127.0.0.1:30303"

	KindMegaphone       = "megaphone"
	KindWebNoticeSingle = "web_notice_single"

	opMegaphone       = 0x0546
	opWebNoticeSingle = 0x09e0
)

type Client struct {
	Address string

	mu       sync.Mutex
	failures int
	retryAt  time.Time
	dial     func(network, address string, timeout time.Duration) (net.Conn, error)
	now      func() time.Time
}

const (
	monitorRetryMin = time.Second
	monitorRetryMax = 30 * time.Second
)

func (c *Client) SendWorldShout(msg, name string, senderID uint16) error {
	return c.SendMonitorAnnouncement(KindMegaphone, msg, name, senderID)
}

func (c *Client) SendMonitorAnnouncement(kind, msg, name string, senderID uint16) error {
	packet, err := BuildAnnouncementPacket(kind, msg, name, senderID)
	if err != nil {
		return err
	}
	return c.send(packet, kind)
}

func (c *Client) send(packet []byte, kind string) error {
	if c == nil {
		return fmt.Errorf("monitor client is nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.now != nil {
		now = c.now()
	}
	if now.Before(c.retryAt) {
		return fmt.Errorf("monitor %s retry is backed off for %s", kind, c.retryAt.Sub(now).Round(time.Millisecond))
	}

	addr := c.Address
	if addr == "" {
		addr = defaultAddress
	}
	dial := c.dial
	if dial == nil {
		dial = net.DialTimeout
	}
	conn, err := dial("tcp", addr, 3*time.Second)
	if err != nil {
		c.recordFailure(now)
		return fmt.Errorf("connect monitor %s: %w", kind, err)
	}
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write(packet); err != nil {
		c.recordFailure(now)
		return fmt.Errorf("send monitor %s: %w", kind, err)
	}
	c.failures = 0
	c.retryAt = time.Time{}
	return nil
}

func (c *Client) recordFailure(now time.Time) {
	c.failures++
	delay := monitorRetryMin
	for i := 1; i < c.failures && delay < monitorRetryMax; i++ {
		delay *= 2
	}
	if delay > monitorRetryMax {
		delay = monitorRetryMax
	}
	c.retryAt = now.Add(delay)
}

type Megaphone = Client

func SendMegaphone(msg, name string, senderID uint16) error {
	return (&Client{}).SendWorldShout(msg, name, senderID)
}

func SendWebNoticeSingle(msg string) error {
	return (&Client{}).SendMonitorAnnouncement(KindWebNoticeSingle, msg, "", 0)
}

func BuildAnnouncementPacket(kind, msg, name string, senderID uint16) ([]byte, error) {
	switch kind {
	case "", KindMegaphone:
		return buildMegaphoneLikePacket(opMegaphone, msg, name, senderID), nil
	case KindWebNoticeSingle:
		return buildWebNoticeSinglePacket(msg), nil
	default:
		return nil, fmt.Errorf("unknown monitor announcement kind %q", kind)
	}
}

// buildMegaphoneLikePacket builds the verified monitor packet that the game
// renders exactly like a normal world megaphone. Keep it as the reference
// implementation for world-shout behavior; system announcements use 0x09e0.
func buildMegaphoneLikePacket(op uint16, msg, name string, senderID uint16) []byte {
	msgBytes := truncateBytes([]byte(msg), 255)
	nameBytes := charset.Windows1252StringBytes(name)
	if len(nameBytes) == 0 {
		nameBytes = []byte("Robot")
	}
	nameBytes = truncateBytes(nameBytes, 0x1e)

	size := 0x2e + len(msgBytes)
	packet := make([]byte, size)
	putHeader(packet, op, size)
	packet[0x0b] = 11
	binary.LittleEndian.PutUint16(packet[0x0c:0x0e], senderID)
	packet[0x0e] = 15
	copy(packet[0x0f:0x2d], nameBytes)
	packet[0x2d] = byte(len(msgBytes))
	copy(packet[0x2e:], msgBytes)
	return packet
}

// buildWebNoticeSinglePacket matches CPacketTranslater::OnWebNoticeSingle:
// monitor forwards the packet header size and logs len at +0x0a, text at +0x0b.
func buildWebNoticeSinglePacket(msg string) []byte {
	msgBytes := truncateBytes([]byte(msg), 255)
	size := 0x0c + len(msgBytes)
	packet := make([]byte, size)
	putHeader(packet, opWebNoticeSingle, size)
	packet[0x0a] = byte(len(msgBytes))
	copy(packet[0x0b:], msgBytes)
	return packet
}

func putHeader(packet []byte, op uint16, size int) {
	binary.LittleEndian.PutUint16(packet[0:2], op)
	binary.LittleEndian.PutUint16(packet[2:4], uint16(size))
}

func truncateBytes(b []byte, max int) []byte {
	if len(b) > max {
		return b[:max]
	}
	return b
}
