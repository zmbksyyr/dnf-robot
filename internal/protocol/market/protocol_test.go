package market

import (
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestSearchByItemKeyPayloadNormal(t *testing.T) {
	payload, err := (SearchByItemKeyRequest{
		PayType:      PayTypeAuction,
		PageOrOffset: 2,
		Mode1:        3,
		Mode2:        4,
		Mode3:        5,
		ItemKeys:     []uint32{100, 200},
		Category1:    -1,
		Category2:    10,
		Category3:    11,
		Filter1:      12,
		Filter2:      13,
	}).Payload()
	if err != nil {
		t.Fatal(err)
	}

	want := "0002000000030402000064000000c8000000ffff0a000b000c0d"
	if got := hex.EncodeToString(payload); got != want {
		t.Fatalf("payload mismatch\n got %s\nwant %s", got, want)
	}
}

func TestSearchByItemKeyPayloadCeraOmitsNormalFilters(t *testing.T) {
	payload, err := (SearchByItemKeyRequest{
		PayType:      PayTypeCera,
		PageOrOffset: 2,
		Mode1:        3,
		Mode2:        4,
		Mode3:        5,
		ItemKeys:     []uint32{100},
		Category1:    -1,
		Filter1:      12,
	}).Payload()
	if err != nil {
		t.Fatal(err)
	}

	want := "0102000000030401000064000000"
	if got := hex.EncodeToString(payload); got != want {
		t.Fatalf("payload mismatch\n got %s\nwant %s", got, want)
	}
}

func TestCeraBiddingPayload(t *testing.T) {
	payload := CeraBiddingRequest{
		CeraAmount: 196,
		AuctionID:  [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
		GoldAmount: 1000000,
	}.Payload()

	want := "01c4000000010203040506070840420f00"
	if got := hex.EncodeToString(payload); got != want {
		t.Fatalf("payload mismatch\n got %s\nwant %s", got, want)
	}
}

func TestAuctionBiddingPayload(t *testing.T) {
	payload := AuctionBiddingRequest{
		Money:       1000,
		AuctionID:   [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
		ExtraBinary: [13]byte{9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21},
	}.Payload()

	want := "00e80300000102030405060708090a0b0c0d0e0f101112131415"
	if got := hex.EncodeToString(payload); got != want {
		t.Fatalf("payload mismatch\n got %s\nwant %s", got, want)
	}
}

func TestBuyItemApiecePayload(t *testing.T) {
	payload := BuyItemApieceRequest{
		Money:       1000,
		Count:       3,
		AuctionID:   [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
		ExtraBinary: [13]byte{9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21},
	}.Payload()

	want := "e8030000030000000102030405060708090a0b0c0d0e0f101112131415"
	if got := hex.EncodeToString(payload); got != want {
		t.Fatalf("payload mismatch\n got %s\nwant %s", got, want)
	}
}

func TestRegisterAuctionItemPayload(t *testing.T) {
	payload := RegisterAuctionItemRequest{
		InventorySpace: 3,
		InventorySlot:  105,
		ItemID:         3037,
		CountOrAddInfo: 10,
		StartPrice:     100,
		InstantPrice:   1000,
		UnitPrice:      100,
		ROICategory:    [3]int16{-1, 2, 3},
		ROIGrade:       [3]byte{4, 5, 6},
	}.Payload()

	want := "00036900dd0b00000a00000064000000e803000064000000ffff02000300040506"
	if got := hex.EncodeToString(payload); got != want {
		t.Fatalf("payload mismatch\n got %s\nwant %s", got, want)
	}
}

func TestDirectRegisterItemGAPacket(t *testing.T) {
	packet := DirectRegisterItemGARequest{
		CharacNo:       1,
		OwnerID:        90000001,
		OwnerName:      "market",
		ItemID:         3037,
		CountOrAddInfo: 17,
		StartPrice:     9000,
		InstantPrice:   10000,
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

func TestDirectSearchByItemKeyGAPacket(t *testing.T) {
	packet, err := (DirectSearchByItemKeyGARequest{
		CharacNo: 1,
		OwnerID:  2,
		ItemKeys: []uint32{7441},
	}).Packet()
	if err != nil {
		t.Fatal(err)
	}
	if len(packet) != 0x35 {
		t.Fatalf("packet length = %d, want %d", len(packet), 0x35)
	}
	want := "000635000000000000000000000000000000010000000200000000000000001f0100000000000000000000000000000007111d0000"
	if got := hex.EncodeToString(packet); got != want {
		t.Fatalf("packet mismatch\n got %s\nwant %s", got, want)
	}
}
