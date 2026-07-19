package auction

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	marketproto "robot/internal/protocol/market"
)

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

func isDirectResultCategory(category byte) bool {
	return category == marketproto.DirectResultCategoryAG || category == marketproto.DirectResultCategoryPG
}
