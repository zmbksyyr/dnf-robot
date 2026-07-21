package auction

import (
	"encoding/binary"
	"encoding/hex"
	"net"
	"testing"
	"time"

	marketproto "robot/internal/protocol/market"
)

func TestReadDirectRegisterAuctionID(t *testing.T) {
	raw, err := hex.DecodeString("130b3600000000000000c700000000000000e24c5d05001500000000000000e24c5d05ffffffff91d22800000100000000000000000013041c00000000000000c800000000000000e24c5d05e24c5d050100")
	if err != nil {
		t.Fatal(err)
	}
	server, client := net.Pipe()
	defer client.Close()
	go func() {
		defer server.Close()
		_, _ = server.Write(raw)
	}()

	var result MarketDirectAuctionResult
	if err := readDirectAuctionPackets(client, time.Now().Add(time.Second), &result, marketproto.DirectResultRegisterItemAG); err != nil {
		t.Fatal(err)
	}
	if result.AuctionID != 21 {
		t.Fatalf("auction id = %d, want 21", result.AuctionID)
	}
	if result.ResultOK == nil || !*result.ResultOK {
		t.Fatalf("result ok = %#v", result.ResultOK)
	}
}

func TestReadDirectRegisterAuctionIDForAuctionService(t *testing.T) {
	raw, err := hex.DecodeString("010b3700000000000000421f000000000000814a5d05000000000000000000814a5d05672d6339cb65000000f40100000000f40100000001041c00000000000000431f000000000000814a5d05814a5d050100")
	if err != nil {
		t.Fatal(err)
	}
	server, client := net.Pipe()
	defer client.Close()
	go func() {
		defer server.Close()
		_, _ = server.Write(raw)
	}()

	var result MarketDirectAuctionResult
	if err := readDirectAuctionPackets(client, time.Now().Add(time.Second), &result, marketproto.DirectResultRegisterItemAG); err != nil {
		t.Fatal(err)
	}
	if result.AuctionID != 8002 {
		t.Fatalf("auction id = %d, want 8002", result.AuctionID)
	}
	if result.ResultOK == nil || !*result.ResultOK {
		t.Fatalf("result ok = %#v", result.ResultOK)
	}
}

func TestBuildDirectRegisterPacketPreservesEquipFields(t *testing.T) {
	packet, err := buildDirectRegisterPacket(MarketDirectRegisterItemRequest{
		CID:            90000001,
		OwnerID:        90000001,
		ItemID:         10068,
		CountOrAddInfo: 0,
		ItemAttr:       12,
		Endurance:      345,
		ExtraAddInfo:   6,
		StartPrice:     999,
		InstantPrice:   1000,
		UnitPrice:      123,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := packet[0x35]; got != 12 {
		t.Fatalf("item attr = %d, want 12", got)
	}
	if got := binary.LittleEndian.Uint16(packet[0x3a:0x3c]); got != 345 {
		t.Fatalf("endurance = %d, want 345", got)
	}
	if got := int32(binary.LittleEndian.Uint32(packet[0x3c:0x40])); got != 6 {
		t.Fatalf("extra add info = %d, want 6", got)
	}
	if got := int32(binary.LittleEndian.Uint32(packet[0x95:0x99])); got != 123 {
		t.Fatalf("unit price = %d, want 123", got)
	}
}
