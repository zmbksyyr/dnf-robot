package market

import "encoding/binary"

const (
	DirectCategoryGA             byte = 0
	DirectCategoryGP             byte = 18
	DirectResultCategoryAG       byte = 1
	DirectResultCategoryPG       byte = 19
	DirectPacketRegisterService  byte = 0
	DirectPacketRegisterItemGA   byte = 3
	DirectPacketBiddingGA        byte = 5
	DirectResultRegisterItemAG   byte = 4
	DirectResultBiddingAG        byte = 5
	DirectPacketHeaderSize            = 10
	DirectRegisterItemPacketSize      = 0xc5
	DirectBiddingPacketSize           = 0x33
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
	HasEndurance   bool
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
	if r.HasEndurance {
		binary.LittleEndian.PutUint16(buf[0x3a:0x3c], r.Endurance)
	}
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

func copyCString(dst []byte, s string) {
	if len(dst) == 0 {
		return
	}
	n := copy(dst, []byte(s))
	if n < len(dst) {
		dst[n] = 0
	}
}
