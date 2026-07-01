package service

import (
	"encoding/hex"
	"net"
	"testing"
	"time"

	marketproto "robot/internal/market"
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
