package auction

import marketproto "robot/internal/protocol/market"

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
