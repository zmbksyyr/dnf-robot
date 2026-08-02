package monitor

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

type partialMonitorConn struct {
	bytes.Buffer
}

func (c *partialMonitorConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (c *partialMonitorConn) Close() error                     { return nil }
func (c *partialMonitorConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (c *partialMonitorConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (c *partialMonitorConn) SetDeadline(time.Time) error      { return nil }
func (c *partialMonitorConn) SetReadDeadline(time.Time) error  { return nil }
func (c *partialMonitorConn) SetWriteDeadline(time.Time) error { return nil }
func (c *partialMonitorConn) Write(data []byte) (int, error) {
	if len(data) > 3 {
		data = data[:3]
	}
	return c.Buffer.Write(data)
}

func TestBuildMegaphonePacket(t *testing.T) {
	packet, err := BuildAnnouncementPacket(KindMegaphone, "hello", "robot", 7)
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint16(packet[0:2]); got != opMegaphone {
		t.Fatalf("op got %#x want %#x", got, opMegaphone)
	}
	if got := int(binary.LittleEndian.Uint16(packet[2:4])); got != len(packet) {
		t.Fatalf("size got %d want %d", got, len(packet))
	}
	if packet[0x0b] != 11 || binary.LittleEndian.Uint16(packet[0x0c:0x0e]) != 7 || packet[0x2d] != 5 {
		t.Fatalf("unexpected megaphone payload")
	}
}

func TestClientBacksOffSharedMonitorFailures(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	calls := 0
	client := &Client{
		Address: "127.0.0.1:30303",
		now:     func() time.Time { return now },
		dial: func(string, string, time.Duration) (net.Conn, error) {
			calls++
			return nil, errors.New("connection refused")
		},
	}
	if err := client.SendWorldShout("hello", "robot", 1); err == nil {
		t.Fatal("first monitor failure was not returned")
	}
	if err := client.SendWorldShout("hello", "robot", 1); err == nil {
		t.Fatal("backoff failure was not returned")
	}
	if calls != 1 {
		t.Fatalf("dial calls during backoff = %d, want 1", calls)
	}

	now = now.Add(monitorRetryMin)
	if err := client.SendWorldShout("hello", "robot", 1); err == nil {
		t.Fatal("retry failure was not returned")
	}
	if calls != 2 {
		t.Fatalf("dial calls after retry = %d, want 2", calls)
	}
	if got := client.retryAt.Sub(now); got != 2*monitorRetryMin {
		t.Fatalf("second retry delay = %s, want %s", got, 2*monitorRetryMin)
	}
}

func TestClientCompletesPartialMonitorWrites(t *testing.T) {
	conn := &partialMonitorConn{}
	client := &Client{dial: func(string, string, time.Duration) (net.Conn, error) { return conn, nil }}
	packet, err := BuildAnnouncementPacket(KindMegaphone, "hello", "robot", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SendWorldShout("hello", "robot", 1); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(conn.Bytes(), packet) {
		t.Fatalf("monitor wrote %d bytes, want %d", conn.Len(), len(packet))
	}
}

func TestClientDoesNotHoldStateLockAcrossNetworkIO(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	client := &Client{dial: func(string, string, time.Duration) (net.Conn, error) {
		started <- struct{}{}
		<-release
		return &partialMonitorConn{}, nil
	}}

	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			if err := client.SendWorldShout("hello", "robot", 1); err != nil {
				t.Errorf("SendWorldShout: %v", err)
			}
		}()
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("monitor sends were serialized behind the state lock")
		}
	}
	close(release)
	wg.Wait()
}

func TestBuildWebNoticePacket(t *testing.T) {
	web, err := BuildAnnouncementPacket(KindWebNoticeSingle, "web", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(web) != 0x0f {
		t.Fatalf("web size got %d want %d", len(web), 0x0f)
	}
	if got := binary.LittleEndian.Uint16(web[0:2]); got != opWebNoticeSingle {
		t.Fatalf("web op got %#x want %#x", got, opWebNoticeSingle)
	}
	if web[0x0a] != 3 || string(web[0x0b:0x0e]) != "web" || web[0x0e] != 0 {
		t.Fatalf("unexpected web payload")
	}
}
