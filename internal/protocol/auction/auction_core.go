package auction

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	marketproto "robot/internal/protocol/market"
	"strconv"
	"strings"
	"time"
)

// ---- protocol.go ----

// ---- market.go ----
type MarketDirectRegisterItemRequest struct {
	Host           string   `json:"host,omitempty"`
	Port           int      `json:"port,omitempty"`
	Point          bool     `json:"point,omitempty"`
	RegisterFirst  bool     `json:"register_first,omitempty"`
	CID            uint32   `json:"cid"`
	OwnerID        uint32   `json:"owner_id"`
	OwnerName      string   `json:"owner_name,omitempty"`
	OwnerType      byte     `json:"owner_type,omitempty"`
	ItemID         uint32   `json:"item_id"`
	CountOrAddInfo int32    `json:"count_or_add_info"`
	ItemType       byte     `json:"item_type,omitempty"`
	ItemAttr       byte     `json:"item_attr,omitempty"`
	Endurance      uint16   `json:"endurance,omitempty"`
	ExtraAddInfo   int32    `json:"extra_add_info,omitempty"`
	StartPrice     int32    `json:"start_price"`
	InstantPrice   int32    `json:"instant_price"`
	ROICategory    [3]int16 `json:"roi_category,omitempty"`
	ROIGrade       [3]byte  `json:"roi_grade,omitempty"`
	TimeoutMS      int      `json:"timeout_ms,omitempty"`
}

type MarketDirectRegisterGoldRequest struct {
	Host       string `json:"host,omitempty"`
	Port       int    `json:"port,omitempty"`
	CID        uint32 `json:"cid"`
	OwnerID    uint32 `json:"owner_id,omitempty"`
	OwnerName  string `json:"owner_name,omitempty"`
	OwnerType  byte   `json:"owner_type,omitempty"`
	ItemID     uint32 `json:"item_id,omitempty"`
	GoldAmount int32  `json:"gold_amount,omitempty"`
	CeraPrice  int32  `json:"cera_price"`
	TimeoutMS  int    `json:"timeout_ms,omitempty"`
}

type MarketDirectBidRequest struct {
	Host          string `json:"host,omitempty"`
	Port          int    `json:"port,omitempty"`
	Point         bool   `json:"point,omitempty"`
	RegisterFirst bool   `json:"register_first,omitempty"`
	CID           uint32 `json:"cid"`
	BuyerID       uint32 `json:"buyer_id"`
	BuyerName     string `json:"buyer_name,omitempty"`
	Money         int32  `json:"money"`
	AuctionID     uint64 `json:"auction_id"`
	TimeoutMS     int    `json:"timeout_ms,omitempty"`
}

type MarketDirectAuctionResult struct {
	Host         string                      `json:"host"`
	Port         int                         `json:"port"`
	Packets      []MarketDirectAuctionPacket `json:"packets"`
	AuctionID    uint64                      `json:"auction_id,omitempty"`
	ResultOK     *bool                       `json:"result_ok,omitempty"`
	ResultReason *byte                       `json:"result_reason,omitempty"`
	BodyHex      string                      `json:"body_hex,omitempty"`
}

type MarketDirectAuctionPacket struct {
	Category byte   `json:"category"`
	PacketID byte   `json:"packet_id"`
	Size     uint32 `json:"size"`
	Hex      string `json:"hex"`
}

type MarketDirectAuctionSession struct {
	conn    net.Conn
	host    string
	port    int
	timeout time.Duration
}

type Session = MarketDirectAuctionSession
type Result = MarketDirectAuctionResult
type RegisterItemRequest = MarketDirectRegisterItemRequest
type RegisterGoldRequest = MarketDirectRegisterGoldRequest
type BidRequest = MarketDirectBidRequest

type RobotSvc struct{}

func NewRobotSvc() *RobotSvc {
	return &RobotSvc{}
}

func NewSession(host string, port, timeoutMS int, point, registerFirst bool) (*Session, error) {
	return NewMarketDirectAuctionSession(host, port, timeoutMS, point, registerFirst)
}

func (rs *RobotSvc) MarketDirectRegisterItem(req MarketDirectRegisterItemRequest) (MarketDirectAuctionResult, error) {
	packet, err := buildDirectRegisterPacket(req)
	if err != nil {
		return MarketDirectAuctionResult{}, err
	}
	return sendDirectAuction(req.Host, directAuctionPort(req.Port, req.Point), req.TimeoutMS, directPrelude(req.Point, req.RegisterFirst), packet, marketproto.DirectResultRegisterItemAG)
}

func (rs *RobotSvc) MarketDirectRegisterGold(req MarketDirectRegisterGoldRequest) (MarketDirectAuctionResult, error) {
	itemID := req.ItemID
	if itemID == 0 {
		itemID = 2675345
	}
	goldAmount := req.GoldAmount
	if goldAmount <= 0 {
		goldAmount = 1
	}
	return rs.MarketDirectRegisterItem(MarketDirectRegisterItemRequest{
		Host:           req.Host,
		Port:           req.Port,
		Point:          true,
		CID:            req.CID,
		OwnerID:        req.OwnerID,
		OwnerName:      req.OwnerName,
		OwnerType:      req.OwnerType,
		ItemID:         itemID,
		CountOrAddInfo: goldAmount,
		StartPrice:     -1,
		InstantPrice:   req.CeraPrice,
		TimeoutMS:      req.TimeoutMS,
	})
}

func (rs *RobotSvc) MarketDirectBid(req MarketDirectBidRequest) (MarketDirectAuctionResult, error) {
	packet, err := buildDirectBidPacket(req)
	if err != nil {
		return MarketDirectAuctionResult{}, err
	}
	return sendDirectAuction(req.Host, directAuctionPort(req.Port, req.Point), req.TimeoutMS, directPrelude(req.Point, req.RegisterFirst), packet, marketproto.DirectResultBiddingAG)
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

func buildDirectRegisterPacket(req MarketDirectRegisterItemRequest) ([]byte, error) {
	if req.CID == 0 {
		return nil, errors.New("cid is required")
	}
	if req.OwnerID == 0 {
		req.OwnerID = req.CID
	}
	if req.ItemID == 0 {
		return nil, errors.New("item_id is required")
	}
	if req.CountOrAddInfo < 0 {
		req.CountOrAddInfo = 1
	}
	if req.InstantPrice != -1 && req.StartPrice >= req.InstantPrice {
		return nil, errors.New("start_price must be less than instant_price")
	}
	ownerName := strings.TrimSpace(req.OwnerName)
	if ownerName == "" {
		ownerName = "market"
	}
	return (marketproto.DirectRegisterItemGARequest{
		Category:       directAuctionCategory(req.Point),
		CharacNo:       req.CID,
		OwnerID:        req.OwnerID,
		OwnerName:      ownerName,
		OwnerType:      req.OwnerType,
		ItemID:         req.ItemID,
		CountOrAddInfo: req.CountOrAddInfo,
		ItemType:       req.ItemType,
		ItemAttr:       req.ItemAttr,
		Endurance:      req.Endurance,
		ExtraAddInfo:   req.ExtraAddInfo,
		StartPrice:     req.StartPrice,
		InstantPrice:   req.InstantPrice,
		ROICategory:    req.ROICategory,
		ROIGrade:       req.ROIGrade,
	}).Packet(), nil
}

func buildDirectBidPacket(req MarketDirectBidRequest) ([]byte, error) {
	if req.CID == 0 {
		return nil, errors.New("cid is required")
	}
	if req.BuyerID == 0 {
		req.BuyerID = req.CID
	}
	if req.AuctionID == 0 {
		return nil, errors.New("auction_id is required")
	}
	if req.Money <= 0 {
		return nil, errors.New("money is required")
	}
	buyerName := strings.TrimSpace(req.BuyerName)
	if buyerName == "" {
		buyerName = "market"
	}
	return (marketproto.DirectBiddingGARequest{
		Category:  directAuctionCategory(req.Point),
		CharacNo:  req.CID,
		BuyerID:   req.BuyerID,
		BuyerName: buyerName,
		Money:     req.Money,
		AuctionID: req.AuctionID,
	}).Packet(), nil
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

func readDirectAuctionPackets(conn net.Conn, deadline time.Time, result *MarketDirectAuctionResult, wantPacketID byte) error {
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		if len(result.Packets) > 0 && hasDirectPacket(result.Packets, wantPacketID) {
			break
		}
		n, err := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			for {
				if len(buf) < marketproto.DirectPacketHeaderSize {
					break
				}
				size := binary.LittleEndian.Uint32(buf[2:6])
				if size < marketproto.DirectPacketHeaderSize {
					return fmt.Errorf("invalid direct auction packet size %d", size)
				}
				if int(size) > len(buf) {
					break
				}
				raw := append([]byte(nil), buf[:size]...)
				result.Packets = append(result.Packets, MarketDirectAuctionPacket{
					Category: raw[0],
					PacketID: raw[1],
					Size:     size,
					Hex:      hex.EncodeToString(raw),
				})
				parseDirectResult(raw, result)
				buf = buf[size:]
			}
			continue
		}
		if err != nil {
			if len(result.Packets) > 0 {
				break
			}
			return err
		}
		if time.Now().After(deadline) {
			break
		}
	}
	return nil
}

func parseDirectResult(raw []byte, result *MarketDirectAuctionResult) {
	if !isDirectResultCategory(raw[0]) {
		return
	}
	if raw[1] == 11 && result.AuctionID == 0 {
		switch raw[0] {
		case marketproto.DirectResultCategoryAG:
			if len(raw) >= 18 {
				result.AuctionID = binary.LittleEndian.Uint64(raw[10:18])
			}
		case marketproto.DirectResultCategoryPG:
			if len(raw) >= 31 {
				result.AuctionID = binary.LittleEndian.Uint64(raw[23:31])
			}
		}
	}
	switch raw[1] {
	case marketproto.DirectResultRegisterItemAG:
		if len(raw) >= 28 {
			ok := raw[26] != 0
			reason := raw[27]
			result.ResultOK = &ok
			result.ResultReason = &reason
			result.BodyHex = hex.EncodeToString(raw[marketproto.DirectPacketHeaderSize:])
		}
	case marketproto.DirectResultBiddingAG:
		if len(raw) >= 32 {
			ok := raw[30] != 0
			reason := raw[31]
			result.ResultOK = &ok
			result.ResultReason = &reason
			result.BodyHex = hex.EncodeToString(raw[marketproto.DirectPacketHeaderSize:])
		}
	}
}

func hasDirectPacket(packets []MarketDirectAuctionPacket, packetID byte) bool {
	if packetID == 0 {
		return len(packets) > 0
	}
	for _, packet := range packets {
		if isDirectResultCategory(packet.Category) && packet.PacketID == packetID {
			return true
		}
	}
	return false
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

func isDirectResultCategory(category byte) bool {
	return category == marketproto.DirectResultCategoryAG || category == marketproto.DirectResultCategoryPG
}
