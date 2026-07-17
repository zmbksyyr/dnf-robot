package dnf

import (
	"net"
	"sync"
)

const partyRoute0Port = 5063

// PartyRoute0Sink prevents the fixed client route from being assigned to a
// single robot. Per-robot party traffic uses the advertised dynamic UDP port.
type PartyRoute0Sink struct {
	conn      *net.UDPConn
	closeOnce sync.Once
	closeErr  error
}

func StartPartyRoute0Sink() (*PartyRoute0Sink, error) {
	return startPartyRoute0Sink(&net.UDPAddr{IP: net.IPv4zero, Port: partyRoute0Port})
}

func startPartyRoute0Sink(addr *net.UDPAddr) (*PartyRoute0Sink, error) {
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return nil, err
	}
	sink := &PartyRoute0Sink{conn: conn}
	go sink.discardLoop()
	return sink, nil
}

func (s *PartyRoute0Sink) discardLoop() {
	buf := make([]byte, 4096)
	for {
		if _, _, err := s.conn.ReadFromUDP(buf); err != nil {
			return
		}
	}
}

func (s *PartyRoute0Sink) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.closeErr = s.conn.Close()
	})
	return s.closeErr
}
