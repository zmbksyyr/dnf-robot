package auction

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	marketproto "robot/internal/protocol/market"
)

type MarketDirectAuctionSession struct {
	conn    net.Conn
	host    string
	port    int
	timeout time.Duration
}

func NewMarketDirectAuctionSession(host string, port int, timeoutMS int, point bool, registerFirst bool) (*MarketDirectAuctionSession, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		host = "127.0.0.1"
	}
	port = directAuctionPort(port, point)
	if port < 0 || port > 65535 {
		return nil, fmt.Errorf("invalid port %d", port)
	}
	timeout := time.Duration(timeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), timeout)
	if err != nil {
		return nil, err
	}
	s := &MarketDirectAuctionSession{conn: conn, host: host, port: port, timeout: timeout}
	prelude := directPrelude(point, registerFirst)
	if len(prelude) > 0 {
		result := MarketDirectAuctionResult{Host: host, Port: port}
		deadline := time.Now().Add(timeout)
		if err := conn.SetDeadline(deadline); err != nil {
			_ = conn.Close()
			return nil, err
		}
		if _, err := conn.Write(prelude); err != nil {
			_ = conn.Close()
			return nil, err
		}
		if err := readDirectAuctionPackets(conn, deadline, &result, marketproto.DirectPacketRegisterService); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}
	return s, nil
}

func (s *MarketDirectAuctionSession) Close() error {
	if s == nil || s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

func (s *MarketDirectAuctionSession) RegisterItem(req MarketDirectRegisterItemRequest) (MarketDirectAuctionResult, error) {
	packet, err := buildDirectRegisterPacket(req)
	if err != nil {
		return MarketDirectAuctionResult{}, err
	}
	return s.send(packet, marketproto.DirectResultRegisterItemAG)
}

func (s *MarketDirectAuctionSession) RegisterGold(req MarketDirectRegisterGoldRequest) (MarketDirectAuctionResult, error) {
	if req.CeraPrice <= 0 {
		return MarketDirectAuctionResult{}, errors.New("cera_price is required")
	}
	itemID := req.ItemID
	if itemID == 0 {
		itemID = 2675345
	}
	goldAmount := req.GoldAmount
	if goldAmount <= 0 {
		goldAmount = 1
	}
	return s.RegisterItem(MarketDirectRegisterItemRequest{
		Point:          true,
		CID:            req.CID,
		OwnerID:        req.OwnerID,
		OwnerName:      req.OwnerName,
		OwnerType:      req.OwnerType,
		ItemID:         itemID,
		CountOrAddInfo: goldAmount,
		StartPrice:     -1,
		InstantPrice:   req.CeraPrice,
	})
}

func (s *MarketDirectAuctionSession) Bid(req MarketDirectBidRequest) (MarketDirectAuctionResult, error) {
	packet, err := buildDirectBidPacket(req)
	if err != nil {
		return MarketDirectAuctionResult{}, err
	}
	return s.send(packet, marketproto.DirectResultBiddingAG)
}

func (s *MarketDirectAuctionSession) send(packet []byte, wantPacketID byte) (MarketDirectAuctionResult, error) {
	result := MarketDirectAuctionResult{Host: s.host, Port: s.port}
	deadline := time.Now().Add(s.timeout)
	if err := s.conn.SetDeadline(deadline); err != nil {
		return result, err
	}
	if _, err := s.conn.Write(packet); err != nil {
		return result, err
	}
	if err := readDirectAuctionPackets(s.conn, deadline, &result, wantPacketID); err != nil {
		return result, err
	}
	if wantPacketID != 0 && !hasDirectPacket(result.Packets, wantPacketID) {
		return result, fmt.Errorf("direct auction response packet %d not received", wantPacketID)
	}
	return result, nil
}

func sendDirectAuction(host string, port int, timeoutMS int, prelude []byte, packet []byte, wantPacketID byte) (MarketDirectAuctionResult, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		host = "127.0.0.1"
	}
	if port == 0 {
		port = 30803
	}
	if port < 0 || port > 65535 {
		return MarketDirectAuctionResult{}, fmt.Errorf("invalid port %d", port)
	}
	timeout := time.Duration(timeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	result := MarketDirectAuctionResult{Host: host, Port: port}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), timeout)
	if err != nil {
		return result, err
	}
	defer conn.Close()
	deadline := time.Now().Add(timeout)
	if err := conn.SetDeadline(deadline); err != nil {
		return result, err
	}
	if len(prelude) > 0 {
		if _, err := conn.Write(prelude); err != nil {
			return result, err
		}
		if err := readDirectAuctionPackets(conn, deadline, &result, marketproto.DirectPacketRegisterService); err != nil {
			return result, err
		}
	}
	if _, err := conn.Write(packet); err != nil {
		return result, err
	}
	if err := readDirectAuctionPackets(conn, deadline, &result, wantPacketID); err != nil {
		return result, err
	}
	if wantPacketID != 0 && !hasDirectPacket(result.Packets, wantPacketID) {
		return result, fmt.Errorf("direct auction response packet %d not received", wantPacketID)
	}
	return result, nil
}

func directAuctionCategory(point bool) byte {
	if point {
		return marketproto.DirectCategoryGP
	}
	return marketproto.DirectCategoryGA
}

func directAuctionPort(port int, point bool) int {
	if port != 0 {
		return port
	}
	if point {
		return 30603
	}
	return 30803
}

func directPrelude(point bool, registerFirst bool) []byte {
	if !point && !registerFirst {
		return nil
	}
	return marketproto.DirectRegisterServicePacket(directAuctionCategory(point))
}
