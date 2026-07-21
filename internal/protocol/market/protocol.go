package market

import (
	"encoding/binary"
	"errors"
)

type PayType byte

const (
	PayTypeAuction PayType = 0
	PayTypeCera    PayType = 1
)

const (
	CmdAuctionAskAveragePrice   uint16 = 185
	CmdAuctionRegisterItem      uint16 = 186
	CmdAuctionRegisterCancel    uint16 = 187
	CmdAuctionBidding           uint16 = 188
	CmdAuctionSearchByItemKey   uint16 = 189
	CmdAuctionSearchByNoItemKey uint16 = 190
	CmdAuctionMyRegisteredItem  uint16 = 191
	CmdAuctionMyBiddingInfo     uint16 = 192
	CmdAuctionMyAuctionHistory  uint16 = 193
	CmdAuctionBuyItemApiece     uint16 = 335
)

const (
	DirectCategoryGA              byte = 0
	DirectCategoryGP              byte = 18
	DirectResultCategoryAG        byte = 1
	DirectResultCategoryPG        byte = 19
	DirectPacketRegisterService   byte = 0
	DirectPacketRegisterItemGA    byte = 3
	DirectPacketSearchByItemKeyGA byte = 6
	DirectPacketBiddingGA         byte = 5
	DirectPacketMyRegisteredGA    byte = 8
	DirectResultRegisterItemAG    byte = 4
	DirectResultBiddingAG         byte = 5
	DirectResultSearchItemKeyAG   byte = 7
	DirectResultMyRegisteredAG    byte = 8
	DirectPacketHeaderSize             = 10
	DirectRegisterItemPacketSize       = 0xc5
	DirectBiddingPacketSize            = 0x33
	DirectMyRegisteredPacketSize       = 0x1a
)

func DirectRegisterServicePacket(category byte) []byte {
	buf := make([]byte, 0x12)
	buf[0] = category
	buf[1] = DirectPacketRegisterService
	binary.LittleEndian.PutUint32(buf[2:6], uint32(len(buf)))
	return buf
}

type DirectRegisterItemGARequest struct {
	Category       byte
	CharacNo       uint32
	OwnerID        uint32
	OwnerName      string
	OwnerType      byte
	ItemID         uint32
	CountOrAddInfo int32
	ItemType       byte
	ItemAttr       byte
	Endurance      uint16
	ExtraAddInfo   int32
	StartPrice     int32
	InstantPrice   int32
	UnitPrice      int32
	ROICategory    [3]int16
	ROIGrade       [3]byte
}

func (r DirectRegisterItemGARequest) Packet() []byte {
	buf := make([]byte, DirectRegisterItemPacketSize)
	category := r.Category
	if category == 0 {
		category = DirectCategoryGA
	}
	buf[0] = category
	buf[1] = DirectPacketRegisterItemGA
	binary.LittleEndian.PutUint32(buf[2:6], uint32(len(buf)))
	binary.LittleEndian.PutUint32(buf[0x12:0x16], r.CharacNo)
	binary.LittleEndian.PutUint32(buf[0x16:0x1a], r.OwnerID)
	copyCString(buf[0x1a:0x27], r.OwnerName)
	buf[0x27] = r.OwnerType
	binary.LittleEndian.PutUint32(buf[0x28:0x2c], uint32(r.StartPrice))
	binary.LittleEndian.PutUint32(buf[0x2c:0x30], uint32(r.InstantPrice))
	buf[0x30] = r.ItemType
	binary.LittleEndian.PutUint32(buf[0x31:0x35], r.ItemID)
	buf[0x35] = r.ItemAttr
	binary.LittleEndian.PutUint32(buf[0x36:0x3a], uint32(r.CountOrAddInfo))
	binary.LittleEndian.PutUint16(buf[0x3a:0x3c], r.Endurance)
	extraAddInfo := r.ExtraAddInfo
	if extraAddInfo == 0 {
		extraAddInfo = r.CountOrAddInfo
	}
	binary.LittleEndian.PutUint32(buf[0x3c:0x40], uint32(extraAddInfo))
	binary.LittleEndian.PutUint32(buf[0x95:0x99], uint32(r.UnitPrice))
	off := 0x99
	for _, category := range r.ROICategory {
		binary.LittleEndian.PutUint16(buf[off:off+2], uint16(category))
		off += 2
	}
	copy(buf[off:off+len(r.ROIGrade)], r.ROIGrade[:])
	return buf
}

type DirectBiddingGARequest struct {
	Category  byte
	CharacNo  uint32
	BuyerID   uint32
	BuyerName string
	Money     int32
	AuctionID uint64
}

func (r DirectBiddingGARequest) Packet() []byte {
	buf := make([]byte, DirectBiddingPacketSize)
	category := r.Category
	if category == 0 {
		category = DirectCategoryGA
	}
	buf[0] = category
	buf[1] = DirectPacketBiddingGA
	binary.LittleEndian.PutUint32(buf[2:6], uint32(len(buf)))
	binary.LittleEndian.PutUint32(buf[0x12:0x16], r.CharacNo)
	binary.LittleEndian.PutUint32(buf[0x16:0x1a], r.BuyerID)
	copyCString(buf[0x1a:0x27], r.BuyerName)
	binary.LittleEndian.PutUint32(buf[0x27:0x2b], uint32(r.Money))
	binary.LittleEndian.PutUint64(buf[0x2b:0x33], r.AuctionID)
	return buf
}

type DirectMyRegisteredGARequest struct {
	Category byte
	CharacNo uint32
	OwnerID  uint32
}

func (r DirectMyRegisteredGARequest) Packet() []byte {
	buf := make([]byte, DirectMyRegisteredPacketSize)
	category := r.Category
	if category == 0 {
		category = DirectCategoryGA
	}
	buf[0] = category
	buf[1] = DirectPacketMyRegisteredGA
	binary.LittleEndian.PutUint32(buf[2:6], uint32(len(buf)))
	binary.LittleEndian.PutUint32(buf[0x12:0x16], r.CharacNo)
	binary.LittleEndian.PutUint32(buf[0x16:0x1a], r.OwnerID)
	return buf
}

type DirectSearchByItemKeyGARequest struct {
	Category byte
	CharacNo uint32
	OwnerID  uint32
	ItemKeys []uint32

	UpgradeMin byte
	UpgradeMax byte
	RefineMin  byte
	RefineMax  byte
	Page       byte
}

func (r DirectSearchByItemKeyGARequest) Packet() ([]byte, error) {
	if len(r.ItemKeys) == 0 {
		return nil, errors.New("item_keys is required")
	}
	if len(r.ItemKeys) > 0xff {
		return nil, errors.New("too many item keys")
	}
	buf := make([]byte, 0x31+len(r.ItemKeys)*4)
	category := r.Category
	if category == 0 {
		category = DirectCategoryGA
	}
	buf[0] = category
	buf[1] = DirectPacketSearchByItemKeyGA
	binary.LittleEndian.PutUint32(buf[2:6], uint32(len(buf)))
	binary.LittleEndian.PutUint32(buf[0x12:0x16], r.CharacNo)
	binary.LittleEndian.PutUint32(buf[0x16:0x1a], r.OwnerID)

	upgradeMax := r.UpgradeMax
	if upgradeMax == 0 {
		upgradeMax = 31
	}
	refineMax := r.RefineMax
	if refineMax == 0 {
		refineMax = 7
	}
	buf[0x1e] = r.UpgradeMin
	buf[0x1f] = upgradeMax
	buf[0x20] = byte(len(r.ItemKeys))
	binary.LittleEndian.PutUint16(buf[0x21:0x23], uint16(r.Page))
	buf[0x2f] = r.RefineMin
	buf[0x30] = refineMax
	for i, key := range r.ItemKeys {
		binary.LittleEndian.PutUint32(buf[0x31+i*4:0x35+i*4], key)
	}
	return buf, nil
}

func copyCString(dst []byte, s string) {
	if len(dst) == 0 {
		return
	}
	n := copy(dst, []byte(s))
	if n < len(dst) {
		dst[n] = 0
	}
}

type SearchByItemKeyRequest struct {
	PayType      PayType
	PageOrOffset uint32
	Mode1        byte
	Mode2        byte
	Mode3        byte
	ItemKeys     []uint32

	Category1 int16
	Category2 int16
	Category3 int16
	Filter1   byte
	Filter2   byte
}

func (r SearchByItemKeyRequest) Payload() ([]byte, error) {
	if len(r.ItemKeys) > 0xffff {
		return nil, errors.New("too many item keys")
	}
	if len(r.ItemKeys) > 0xff {
		return nil, errors.New("too many item keys")
	}
	extra := 0
	if r.PayType != PayTypeCera {
		extra = 8
	}
	b := newBuilder(1 + 4 + 3 + 2 + len(r.ItemKeys)*4 + extra)
	b.byte(byte(r.PayType))
	b.u32(r.PageOrOffset)
	b.byte(r.Mode1)
	upgradeMax := r.Mode2
	if r.PayType != PayTypeCera && upgradeMax == 0 {
		upgradeMax = 31
	}
	b.byte(upgradeMax)
	b.byte(byte(len(r.ItemKeys)))
	b.u16(0)
	for _, key := range r.ItemKeys {
		b.u32(key)
	}
	if r.PayType != PayTypeCera {
		b.i16(r.Category1)
		b.i16(r.Category2)
		b.i16(r.Category3)
		b.byte(r.Filter1)
		refineMax := r.Filter2
		if refineMax == 0 {
			refineMax = 7
		}
		b.byte(refineMax)
	}
	return b.bytes(), nil
}

type SearchByNoItemKeyRequest struct {
	PayType      PayType
	PageOrOffset uint32
	Category     uint16
	Filter1      byte
	Filter2      byte
	Filter3      byte
	Filter4      byte
	Filter5      byte

	Category1 int16
	Category2 int16
	Category3 int16
	Filter6   byte
	Filter7   byte
}

func (r SearchByNoItemKeyRequest) Payload() []byte {
	b := newBuilder(1 + 4 + 2 + 5 + 8)
	b.byte(byte(r.PayType))
	b.u32(r.PageOrOffset)
	b.u16(r.Category)
	b.byte(r.Filter1)
	b.byte(r.Filter2)
	b.byte(r.Filter3)
	b.byte(r.Filter4)
	b.byte(r.Filter5)
	if r.PayType != PayTypeCera {
		b.i16(r.Category1)
		b.i16(r.Category2)
		b.i16(r.Category3)
		b.byte(r.Filter6)
		b.byte(r.Filter7)
	}
	return b.bytes()
}

type RegisterCancelRequest struct {
	PayType   PayType
	AuctionID [8]byte
}

func (r RegisterCancelRequest) Payload() []byte {
	b := newBuilder(9)
	b.byte(byte(r.PayType))
	b.raw(r.AuctionID[:])
	return b.bytes()
}

type MyMarketRequest struct {
	PayType PayType
}

func (r MyMarketRequest) Payload() []byte {
	return []byte{byte(r.PayType)}
}

type CeraBiddingRequest struct {
	CeraAmount uint32
	AuctionID  [8]byte
	GoldAmount int32
}

func (r CeraBiddingRequest) Payload() []byte {
	b := newBuilder(1 + 4 + 8 + 4)
	b.byte(byte(PayTypeCera))
	b.u32(r.CeraAmount)
	b.raw(r.AuctionID[:])
	b.i32(r.GoldAmount)
	return b.bytes()
}

type AuctionBiddingRequest struct {
	Money       int32
	AuctionID   [8]byte
	ExtraBinary [13]byte
}

func (r AuctionBiddingRequest) Payload() []byte {
	b := newBuilder(1 + 4 + 8 + 13)
	b.byte(byte(PayTypeAuction))
	b.i32(r.Money)
	b.raw(r.AuctionID[:])
	b.raw(r.ExtraBinary[:])
	return b.bytes()
}

type BuyItemApieceRequest struct {
	Money       int32
	Count       int32
	AuctionID   [8]byte
	ExtraBinary [13]byte
}

func (r BuyItemApieceRequest) Payload() []byte {
	b := newBuilder(4 + 4 + 8 + 13)
	b.i32(r.Money)
	b.i32(r.Count)
	b.raw(r.AuctionID[:])
	b.raw(r.ExtraBinary[:])
	return b.bytes()
}

type RegisterCeraItemRequest struct {
	InventorySpace byte
	InventorySlot  uint16
	ItemID         uint32
	CountOrAddInfo int32
	UnitPrice      int32
	InstantPrice   int32
}

func (r RegisterCeraItemRequest) Payload() []byte {
	b := newBuilder(1 + 1 + 2 + 4 + 4 + 4 + 4)
	b.byte(byte(PayTypeCera))
	b.byte(r.InventorySpace)
	b.u16(r.InventorySlot)
	b.u32(r.ItemID)
	b.i32(r.CountOrAddInfo)
	b.i32(r.UnitPrice)
	b.i32(r.InstantPrice)
	return b.bytes()
}

type RegisterAuctionItemRequest struct {
	InventorySpace byte
	InventorySlot  uint16
	ItemID         uint32
	CountOrAddInfo int32
	StartPrice     int32
	InstantPrice   int32
	UnitPrice      int32
	ROICategory    [3]int16
	ROIGrade       [3]byte
}

func (r RegisterAuctionItemRequest) Payload() []byte {
	b := newBuilder(1 + 1 + 2 + 4 + 4 + 4 + 4 + 4 + 6 + 3)
	b.byte(byte(PayTypeAuction))
	b.byte(r.InventorySpace)
	b.u16(r.InventorySlot)
	b.u32(r.ItemID)
	b.i32(r.CountOrAddInfo)
	b.i32(r.StartPrice)
	b.i32(r.InstantPrice)
	b.i32(r.UnitPrice)
	for _, category := range r.ROICategory {
		b.i16(category)
	}
	for _, grade := range r.ROIGrade {
		b.byte(grade)
	}
	return b.bytes()
}

type builder struct {
	buf []byte
}

func newBuilder(capacity int) *builder {
	return &builder{buf: make([]byte, 0, capacity)}
}

func (b *builder) bytes() []byte {
	out := make([]byte, len(b.buf))
	copy(out, b.buf)
	return out
}

func (b *builder) raw(v []byte) {
	b.buf = append(b.buf, v...)
}

func (b *builder) byte(v byte) {
	b.buf = append(b.buf, v)
}

func (b *builder) u16(v uint16) {
	var tmp [2]byte
	binary.LittleEndian.PutUint16(tmp[:], v)
	b.raw(tmp[:])
}

func (b *builder) i16(v int16) {
	b.u16(uint16(v))
}

func (b *builder) u32(v uint32) {
	var tmp [4]byte
	binary.LittleEndian.PutUint32(tmp[:], v)
	b.raw(tmp[:])
}

func (b *builder) i32(v int32) {
	b.u32(uint32(v))
}
