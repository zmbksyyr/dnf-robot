package dnf

import (
	"net"
	"testing"
	"time"
)

func TestPartyRoute0SinkDropsDatagrams(t *testing.T) {
	sink, err := startPartyRoute0Sink(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()

	addr := sink.conn.LocalAddr().(*net.UDPAddr)
	if duplicate, err := net.ListenUDP("udp4", addr); err == nil {
		duplicate.Close()
		t.Fatal("route0 sink did not retain exclusive ownership of its UDP port")
	}

	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.WriteToUDP([]byte("route0 probe"), addr); err != nil {
		t.Fatal(err)
	}
	if err := client.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	if _, _, err := client.ReadFromUDP(buf); err == nil {
		t.Fatal("route0 sink replied to a discarded datagram")
	} else if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
		t.Fatalf("read error = %v, want timeout", err)
	}
}

func TestPartyRoute0SinkCloseIsIdempotent(t *testing.T) {
	sink, err := startPartyRoute0Sink(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
}
