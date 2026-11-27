package dnf

import (
	"encoding/binary"
	"math/bits"
	"testing"

	"robot/internal/shared"
)

func TestInventoryChecksumMatchesOriginalArithmetic(t *testing.T) {
	// Executed without replacing instructions in DNF.exe 0x125a6e0 and
	// df_game_r getItemChecksum 0x889234d. Include wrapped-negative products.
	for _, test := range []struct {
		slot                             int
		id, quantity, upgrade, endurance int32
		want                             uint32
	}{
		{12, 10400, 1, 0, 33, 59673},
		{3, 690017018, 1, 0, 0, 29779},
		{311, 2147483647, 2147483647, 31, 65535, 9019},
		{3, 2147483647, 1, 0, 0, 0},
		{9, 27600, 1, 31, 65535, 52083},
		{153, 4183, 3, 0, 0, 50643},
		{311, 2147483647, 0, 0, 0, 65224},
		{311, -2147483648, -1, 31, 65535, 9330},
	} {
		if got := inventoryItemChecksum(test.slot, test.id, test.quantity, test.upgrade, test.endurance); got != test.want {
			t.Fatalf("slot %d id %d: checksum %d want %d", test.slot, test.id, got, test.want)
		}
	}
}

func checksumTestRobot(t *testing.T) *RobotVo {
	t.Helper()
	shared.SetInventoryItemKinds([]shared.EquipmentCatalogItem{{ID: 27600}},
		[]shared.EquipmentCatalogItem{{ID: 690017018}, {ID: 4183}, {ID: 10005005}, {ID: 10005023}, {ID: 2749110},
			{ID: 10000162}, {ID: 10000163}, {ID: 10000173}})
	t.Cleanup(func() { shared.SetInventoryItemKinds(nil, nil) })
	r := NewRobotVo(nil)
	r.State, r.Cipher = StateRun, newPartyTestCipher(t)
	r.inventoryChecksumSeed, r.inventoryChecksumSeedKnown = 1955105775, true
	var ok bool
	r.characterBaseInfo, ok = parseCharacterBasicInfo(mustPartyHex(t, capturedCharacterBaseInfo), 2)
	if !ok {
		t.Fatal("captured character info")
	}
	r.parsePacket(inventoryTestPacket(13, 0,
		inventoryTestRecord(0, 0, 0, false), inventoryTestRecord(1, 1, 5, false), inventoryTestRecord(2, 2, 0, false),
		inventoryTestRecord(3, 690017018, 1, false), inventoryTestRecord(9, 27600, 2141828876, false),
		inventoryTestRecord(153, 4183, 3, false), inventoryTestRecord(154, 10005005, 5, false),
		inventoryTestRecord(155, 10005023, 5, false), inventoryTestRecord(156, 2749110, 5, false),
		inventoryTestRecord(157, 10000162, 5, false), inventoryTestRecord(158, 10000163, 5, false),
		inventoryTestRecord(159, 10000173, 5, false)).data)
	return r
}

func TestInventoryChecksumFullCapturedCharacterAndRealHeartbeat(t *testing.T) {
	r := checksumTestRobot(t)
	// Full original client getter returned 1955494751 after all 65,541
	// instructions, including equipment, three currency records and seed.
	if got, known := r.inventoryChecksumUnsafe(); !known || got != 1955494751 {
		t.Fatalf("captured checksum %d known=%t", got, known)
	}
	conn := &captureSessionConn{}
	r.Conn, r.PacketID = conn, 77
	r.parsePacket(make([]byte, 15))
	if len(conn.written) != 21 || r.PacketID != 78 {
		t.Fatalf("heartbeat %x", conn.written)
	}
	body, err := r.Cipher.Decrypt(0, conn.written[13:])
	if err != nil || binary.LittleEndian.Uint32(body) != 1955494751 {
		t.Fatalf("heartbeat body %x err=%v", body, err)
	}
	// A material decrement arrives through the production packet switch.
	r.parsePacket(inventoryTestPacket(14, 0, inventoryTestRecord(153, 4183, 2, false)).data)
	if got, known := r.inventoryChecksumUnsafe(); !known || got != 1955494904 {
		t.Fatalf("updated checksum %d known=%t", got, known)
	}
}

func TestInventoryChecksumUnavailableInputsAndSessionBoundaries(t *testing.T) {
	for name, change := range map[string]func(*RobotVo){
		"missing seed":           func(r *RobotVo) { r.inventoryChecksumSeedKnown = false },
		"missing baseline":       func(r *RobotVo) { r.inventoryChecksumKnown = false },
		"delete acknowledgement": func(r *RobotVo) { r.inventoryNeedsSnapshot = true },
		"unknown equipment":      func(r *RobotVo) { r.characterBaseInfo.equipment.known = false },
		"return pending":         func(r *RobotVo) { r.ReturnSelectPending = true },
		"stale transaction":      func(r *RobotVo) { v := r.InfanMap[153]; v.ItemNum--; r.InfanMap[153] = v },
		"missing currency":       func(r *RobotVo) { delete(r.InfanMap, 2); delete(r.inventoryChecksumRecords, 2) },
		"unknown item":           func(r *RobotVo) { shared.SetInventoryItemKinds(nil, nil) },
		"equipment update": func(r *RobotVo) {
			r.parsePacket(inventoryTestPacket(14, 3, inventoryTestRecord(12, 10400, 1, false)).data)
		},
		"move notification": func(r *RobotVo) { raw := make([]byte, 15); raw[1] = 18; r.parsePacket(raw) },
		"malformed main update": func(r *RobotVo) {
			packet := inventoryTestPacket(14, 0, inventoryTestRecord(153, 4183, 2, false))
			r.parsePacket(packet.data[:len(packet.data)-1])
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := checksumTestRobot(t)
			change(r)
			if _, known := r.inventoryChecksumUnsafe(); known {
				t.Fatal("used unavailable/stale checksum")
			}
		})
	}
	r := checksumTestRobot(t)
	r.finishReturnToSelectUnsafe()
	if !r.inventoryChecksumSeedKnown || r.inventoryChecksumSeed != 1955105775 || r.inventoryChecksumKnown || len(r.inventoryChecksumRecords) != 0 {
		t.Fatal("character return lost connection seed or retained inventory")
	}
	r.closeOutUnsafe()
	if r.inventoryChecksumSeedKnown || r.inventoryChecksumSeed != 0 {
		t.Fatal("closed connection retained seed")
	}
}

func TestInventoryChecksumHandshakeSeed(t *testing.T) {
	body := make([]byte, 334)
	body = append(body, mustPartyHex(t, "0500000063682e3131e9b4000022143d16")...)
	if seed, ok := parseInventoryChecksumSeed(body); !ok || seed != 0x163d1422 {
		t.Fatalf("seed %#x ok=%t", seed, ok)
	}
	for size := 0; size < len(body); size++ {
		if _, ok := parseInventoryChecksumSeed(body[:size]); ok {
			t.Fatalf("accepted truncated seed %d", size)
		}
	}
	binary.LittleEndian.PutUint32(body[334:], 0xffffffff)
	if _, ok := parseInventoryChecksumSeed(body); ok {
		t.Fatal("accepted overflowing channel length")
	}
}

func TestInventoryChecksumSeedThroughLoginPacket(t *testing.T) {
	conn := &captureSessionConn{}
	r := newLoginPacketTestRobot(t, conn)
	body := append(make([]byte, 334), mustPartyHex(t, "0500000063682e3131e9b4000022143d16")...)
	raw := make([]byte, 15+len(body))
	raw[1] = 1
	binary.LittleEndian.PutUint32(raw[3:], uint32(len(raw)))
	// Encode the initial notification with the protocol's fixed login key.
	const key = 71646901
	for i, value := range body {
		raw[15+i] = bits.RotateLeft8(value^byte(key&0xff), -int((key>>8)&7))
	}
	r.parsePacket(raw)
	if !r.inventoryChecksumSeedKnown || r.inventoryChecksumSeed != 0x163d1422 || len(conn.written) == 0 {
		t.Fatalf("initial login seed=%#x known=%t sent=%d", r.inventoryChecksumSeed, r.inventoryChecksumSeedKnown, len(conn.written))
	}
	// Login replies and other notifications must preserve the connection seed.
	reply := make([]byte, 15)
	reply[0], reply[1] = 1, 1
	r.parsePacket(reply)
	if !r.inventoryChecksumSeedKnown || r.inventoryChecksumSeed != 0x163d1422 {
		t.Fatal("login reply cleared connection seed")
	}
	r.State = StateLogin
	r.parsePacket([]byte{0, 1, 0, 7, 0, 0, 0})
	if r.inventoryChecksumSeedKnown {
		t.Fatal("incomplete new handshake retained previous seed")
	}
}
