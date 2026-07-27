package market

import (
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestDirectRegisterItemGAPacket(t *testing.T) {
	packet := DirectRegisterItemGARequest{
		CharacNo:       1,
		OwnerID:        90000001,
		OwnerName:      "market",
		ItemID:         3037,
		CountOrAddInfo: 17,
		StartPrice:     9000,
		InstantPrice:   10000,
		UnitPrice:      588,
		ROICategory:    [3]int16{-1, 26, 21},
		ROIGrade:       [3]byte{0, 0, 0},
	}.Packet()

	if len(packet) != DirectRegisterItemPacketSize {
		t.Fatalf("packet length = %d, want %d", len(packet), DirectRegisterItemPacketSize)
	}
	wantHead := "0003c500000000000000000000000000000001000000814a5d056d61726b6574"
	if got := hex.EncodeToString(packet[:32]); got != wantHead {
		t.Fatalf("packet head mismatch\n got %s\nwant %s", got, wantHead)
	}
	wantItem := "282300001027000000dd0b00000011000000000011000000"
	if got := hex.EncodeToString(packet[0x28:0x40]); got != wantItem {
		t.Fatalf("item segment mismatch\n got %s\nwant %s", got, wantItem)
	}
	if got := int32(binary.LittleEndian.Uint32(packet[0x95:0x99])); got != 588 {
		t.Fatalf("unit price = %d, want 588", got)
	}
	wantROI := "ffff1a001500000000"
	if got := hex.EncodeToString(packet[0x99:0xa2]); got != wantROI {
		t.Fatalf("roi segment mismatch\n got %s\nwant %s", got, wantROI)
	}
}

func TestDirectRegisterItemGAPacketWritesEquipFields(t *testing.T) {
	packet := DirectRegisterItemGARequest{
		CharacNo:       1,
		OwnerID:        90000001,
		OwnerName:      "market",
		ItemID:         10068,
		CountOrAddInfo: 0,
		ItemAttr:       13,
		Endurance:      456,
		HasEndurance:   true,
		ExtraAddInfo:   7,
		StartPrice:     999,
		InstantPrice:   1000,
	}.Packet()

	if got := packet[0x35]; got != 13 {
		t.Fatalf("item attr = %d, want 13", got)
	}
	if got := binary.LittleEndian.Uint16(packet[0x3a:0x3c]); got != 456 {
		t.Fatalf("endurance = %d, want 456", got)
	}
	if got := int32(binary.LittleEndian.Uint32(packet[0x3c:0x40])); got != 7 {
		t.Fatalf("extra add info = %d, want 7", got)
	}
}

func TestDirectRegisterItemGAPacketOmitsEnduranceWithoutFlag(t *testing.T) {
	packet := DirectRegisterItemGARequest{
		ItemID:       3037,
		Endurance:    456,
		HasEndurance: false,
	}.Packet()

	if got := binary.LittleEndian.Uint16(packet[0x3a:0x3c]); got != 0 {
		t.Fatalf("endurance = %d, want omitted zero bytes", got)
	}
}

func TestDirectRegisterGoldPointBuyNowPacket(t *testing.T) {
	packet := DirectRegisterItemGARequest{
		Category:       DirectCategoryGP,
		CharacNo:       3,
		OwnerID:        3,
		OwnerName:      "sysgold",
		ItemID:         2675345,
		CountOrAddInfo: 1,
		StartPrice:     -1,
		InstantPrice:   1200,
	}.Packet()

	if packet[0] != DirectCategoryGP {
		t.Fatalf("category = %d, want %d", packet[0], DirectCategoryGP)
	}
	if got := int32(binary.LittleEndian.Uint32(packet[0x28:0x2c])); got != -1 {
		t.Fatalf("start price = %d, want -1", got)
	}
	if got := int32(binary.LittleEndian.Uint32(packet[0x2c:0x30])); got != 1200 {
		t.Fatalf("instant price = %d, want 1200", got)
	}
	if got := binary.LittleEndian.Uint32(packet[0x31:0x35]); got != 2675345 {
		t.Fatalf("item id = %d, want 2675345", got)
	}
}

func TestDirectBiddingGAPacket(t *testing.T) {
	packet := DirectBiddingGARequest{
		CharacNo:  90000003,
		BuyerID:   90000003,
		BuyerName: "buyerx",
		Money:     10100,
		AuctionID: 9007,
	}.Packet()

	if len(packet) != DirectBiddingPacketSize {
		t.Fatalf("packet length = %d, want %d", len(packet), DirectBiddingPacketSize)
	}
	want := "000533000000000000000000000000000000834a5d05834a5d0562757965727800000000000000742700002f23000000000000"
	if got := hex.EncodeToString(packet); got != want {
		t.Fatalf("packet mismatch\n got %s\nwant %s", got, want)
	}
}
