package auction

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
	UnitPrice      int32    `json:"unit_price,omitempty"`
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

type Session = MarketDirectAuctionSession
type Result = MarketDirectAuctionResult
type RegisterItemRequest = MarketDirectRegisterItemRequest
type RegisterGoldRequest = MarketDirectRegisterGoldRequest
type BidRequest = MarketDirectBidRequest
