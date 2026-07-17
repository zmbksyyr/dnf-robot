package dnf

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"net"
	"testing"
	"time"
)

func TestBuildPeerResponse(t *testing.T) {
	tests := []struct {
		name    string
		request []byte
		typ     byte
		want    []byte
		ok      bool
	}{
		{
			name:    "party",
			request: []byte{0x34, 0x12, peerRequestParty, 0x78, 0x56, 0x34, 0x12, 0xaa},
			typ:     peerRequestParty,
			want:    []byte{0x34, 0x12, peerRequestParty, 0x78, 0x56, 0x34, 0x12, 0},
			ok:      true,
		},
		{
			name:    "trade",
			request: []byte{0xcd, 0xab, peerRequestTrade, 4, 3, 2, 1},
			typ:     peerRequestTrade,
			want:    []byte{0xcd, 0xab, peerRequestTrade, 4, 3, 2, 1, 0},
			ok:      true,
		},
		{name: "short", request: []byte{1, 2, 3}},
		{name: "unsupported", request: []byte{1, 2, 4, 5, 6, 7, 8}, typ: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, typ, ok := buildPeerResponse(tt.request)
			if ok != tt.ok || typ != tt.typ || !bytes.Equal(got, tt.want) {
				t.Fatalf("buildPeerResponse() = %x, %d, %v; want %x, %d, %v", got, typ, ok, tt.want, tt.typ, tt.ok)
			}
		})
	}
}

func TestPartyAcceptGameOptions(t *testing.T) {
	options := make([]byte, gameEtcOptionSize)
	for i := range options {
		options[i] = byte(i + 1)
	}
	packet := make([]byte, 4+len(options)+8)
	binary.LittleEndian.PutUint32(packet[:4], uint32(len(options)))
	copy(packet[4:], options)

	got, ok := partyAcceptGameOptions(packet)
	if !ok {
		t.Fatal("valid game options were rejected")
	}
	want := append([]byte(nil), options...)
	binary.LittleEndian.PutUint16(want[partyRejectOption*2:], 0)
	if !bytes.Equal(got, want) {
		t.Fatalf("options = %x, want %x", got, want)
	}

	for _, invalid := range [][]byte{nil, {1, 2, 3}, {gameEtcOptionSize - 1, 0, 0, 0}, {gameEtcOptionSize, 0, 0, 0, 1}} {
		if _, ok := partyAcceptGameOptions(invalid); ok {
			t.Fatalf("invalid options accepted: %x", invalid)
		}
	}
}

func TestDefaultPartyAcceptGameOptions(t *testing.T) {
	got := defaultPartyAcceptGameOptions()
	if len(got) != gameEtcOptionSize {
		t.Fatalf("length = %d, want %d", len(got), gameEtcOptionSize)
	}
	for i := 0; i < gameEtcOptionSize/2; i++ {
		want := uint16(0x7fff)
		if i == 1 {
			want = 1
		}
		if i == partyRejectOption {
			want = 0
		}
		if value := binary.LittleEndian.Uint16(got[i*2:]); value != want {
			t.Fatalf("option %d = %d, want %d", i, value, want)
		}
	}
}

func TestBuildNATInfoPayload(t *testing.T) {
	got, ok := buildNATInfoPayload(net.IPv4(192, 168, 200, 131), 45678)
	if !ok {
		t.Fatal("IPv4 address was rejected")
	}
	if len(got) != 24 || got[0] != 1 {
		t.Fatalf("payload = %x", got)
	}
	wantIP := []byte{192, 168, 200, 131}
	if !bytes.Equal(got[1:5], wantIP) || !bytes.Equal(got[5:9], wantIP) {
		t.Fatalf("IP fields = %x/%x", got[1:5], got[5:9])
	}
	if !bytes.Equal(got[9:11], []byte{0xb2, 0x6e}) {
		t.Fatalf("network-order port = %x, want b26e", got[9:11])
	}
	if mtu := binary.LittleEndian.Uint32(got[11:15]); mtu != 1472 {
		t.Fatalf("MTU = %d, want 1472", mtu)
	}
	if marker := string(got[19:]); marker != "robot" {
		t.Fatalf("marker = %q", marker)
	}
	if _, ok := buildNATInfoPayload(net.ParseIP("2001:db8::1"), 1234); ok {
		t.Fatal("IPv6 address should be rejected")
	}
}

func TestParsePartyIPInfoSnapshot(t *testing.T) {
	packet := make([]byte, 1+3*22)
	packet[0] = 3
	putPartyPeer(packet[1:23], 0x1111, net.IPv4(192, 168, 200, 1), 5063, 18000000, 1, 1472)
	putPartyPeer(packet[45:67], 0x3333, net.IPv4(192, 168, 200, 131), 45678, 17000001, 1, 1472)

	self, peers, ok := parsePartyIPInfoSnapshot(packet, 17000001)
	if !ok || self.uniqueID != 0x3333 || self.slot != 2 || !self.slotKnown {
		t.Fatalf("self = %+v ok=%v", self, ok)
	}
	if len(peers) != 1 || peers[0].uniqueID != 0x1111 || peers[0].slot != 0 || peers[0].port != 5063 {
		t.Fatalf("peers = %+v", peers)
	}
}

func TestPartyPeerLifecycle(t *testing.T) {
	vo := &RobotVo{}
	vo.setPartyPendingUnsafe(7)
	if !vo.partyActiveUnsafe() {
		t.Fatal("pending invitation should pause automatic actions before the party snapshot")
	}
	if vo.partyPeers[0].uniqueID != 0 {
		t.Fatalf("pending invitation was stored as confirmed peer: %+v", vo.partyPeers[0])
	}
	vo.partyPendingUntil = time.Now().Add(-time.Second)
	if vo.partyActiveUnsafe() || vo.partyPendingPeer != 0 || !vo.partyPendingUntil.IsZero() {
		t.Fatalf("expired pending invitation remained active: peer=%d until=%s", vo.partyPendingPeer, vo.partyPendingUntil)
	}
	vo.setPartyPendingUnsafe(7)
	vo.removePartyPeerUnsafe(99)
	if vo.partyActiveUnsafe() || vo.partyPendingPeer != 0 {
		t.Fatalf("leave notification did not clear pending invitation: peer=%d", vo.partyPendingPeer)
	}

	vo.setPartyPendingUnsafe(7)
	vo.partySelfPeer = partyIPPeer{uniqueID: 9, slot: 1, slotKnown: true}
	vo.setPartyPeersUnsafe([]partyIPPeer{{uniqueID: 7, accID: 18000000, slot: 0, slotKnown: true}})
	if vo.partyPendingPeer != 0 || !vo.partyPendingUntil.IsZero() {
		t.Fatalf("confirmed snapshot did not clear pending invitation: peer=%d until=%s", vo.partyPendingPeer, vo.partyPendingUntil)
	}
	if vo.partyPeers[0].accID != 18000000 || !vo.partyPeers[0].slotKnown {
		t.Fatalf("peer was not enriched: %+v", vo.partyPeers[0])
	}
	vo.removePartyPeerUnsafe(7)
	if vo.partyActiveUnsafe() || vo.partySelfPeer.uniqueID != 0 {
		t.Fatalf("party state was not cleared: self=%+v peers=%+v", vo.partySelfPeer, vo.partyPeers)
	}
}

func TestPartyInfoClearStateResetsFollowState(t *testing.T) {
	vo := &RobotVo{}
	vo.partySelfPeer = partyIPPeer{uniqueID: 9, slot: 1, slotKnown: true}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 7, slot: 0, slotKnown: true}
	vo.townEntityPositions = map[uint16]townEntityPosition{7: {uniqueID: 7}}
	vo.partyDungeonTraceAt = time.Now().Add(time.Minute)

	if partyInfoClearsParty(mustPartyHex(t, "0100220000486b01a86b01")) {
		t.Fatal("active party info was treated as clear")
	}
	if partyInfoClearsParty(mustPartyHex(t, "010014000296b00165b3018bb301ffffff0000000000b73c2e43")) {
		t.Fatal("active three-member party info was treated as clear")
	}
	if !partyInfoClearsParty(mustPartyHex(t, "0100220002ffffff486b01ffffffffffff00010000005887dd13")) {
		t.Fatal("clear party info was not recognized")
	}
	vo.clearPartyUnsafe()
	if vo.partyActiveUnsafe() || len(vo.townEntityPositions) != 0 || !vo.partyDungeonTraceAt.IsZero() {
		t.Fatalf("party state remained after clear: peers=%+v positions=%+v", vo.partyPeers, vo.townEntityPositions)
	}
}

func TestCheckUserStateClosesOnlyIdlePartyRelay(t *testing.T) {
	idleRobot, idlePeer := net.Pipe()
	defer idlePeer.Close()
	idle := &RobotVo{State: StateRun, partyRelayConn: idleRobot}
	if !idle.CheckUserState() {
		t.Fatal("idle running robot was stopped")
	}
	if idle.partyRelayConn != nil {
		t.Fatal("idle party relay remained connected")
	}

	activeRobot, activePeer := net.Pipe()
	defer activeRobot.Close()
	defer activePeer.Close()
	active := &RobotVo{State: StateRun, partyRelayConn: activeRobot}
	active.partyPeers[0] = partyIPPeer{uniqueID: 1}
	if !active.CheckUserState() {
		t.Fatal("grouped running robot was stopped")
	}
	if active.partyRelayConn != activeRobot {
		t.Fatal("active party relay was closed")
	}
}

func TestRobotRewritesCapturedDungeonPosition(t *testing.T) {
	position := mustPartyHex(t, "028703000034000000015100970cfec701070034ede5df0001070034ede5df1102a97492965e0800000d000000ffffffffffffffff0000000000000000")
	body, ok := buildPartyDungeonFollowBody(position, 0x9692, 0x1fab)
	want := mustPartyHex(t, "015100bc9521ba010700f29a59e300010700f29a59e31102a974ab1f5e0800000d000000ffffffffffffffff0000000000000000")
	if !ok || !bytes.Equal(body, want) {
		t.Fatalf("position body = %x ok=%t, want %x", body, ok, want)
	}
}

func TestRobotFlushesOnlyDelayedDungeonInputRecords(t *testing.T) {
	receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	sender, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()

	input1 := mustPartyHex(t, "02270001136ff17e7e7e7e")
	input2 := mustPartyHex(t, "02270001783cf97e7e7e7e")
	skill := mustPartyHex(t, "024400a7eb502bece87e7e7e7e")
	frame := buildPartyReliableRecordBatchPacket(9, 0, 5, [][]byte{input1, skill, input2})
	leaderAddr := receiver.LocalAddr().(*net.UDPAddr)
	vo := &RobotVo{UID: 17000001, partyUDPConn: sender}
	vo.partySelfPeer = partyIPPeer{uniqueID: 0x1fab, accID: 17000001, slot: 1, slotKnown: true}
	leader := partyIPPeer{uniqueID: 0x9692, accID: 18000000, slot: 0, slotKnown: true, outerIP: leaderAddr.IP, port: uint16(leaderAddr.Port)}
	vo.partyPeers[0] = leader
	vo.schedulePartyDungeonInputUnsafe(frame, leader)
	if len(vo.partyDungeonInputs) != 2 || vo.partyDungeonInputAt.IsZero() {
		t.Fatalf("scheduled inputs=%x at=%s", vo.partyDungeonInputs, vo.partyDungeonInputAt)
	}
	vo.partyDungeonInputAt = time.Now().Add(-time.Millisecond)
	vo.flushPartyDungeonInputUnsafe(sender)
	if len(vo.partyDungeonInputs) != 0 || !vo.partyDungeonInputAt.IsZero() {
		t.Fatal("flushed input batch remained pending")
	}

	if err := receiver.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 128)
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	got := buf[:n]
	if got[0] != 0x01 || got[7] != 1 || got[8] != 5 || binary.LittleEndian.Uint32(got[1:5]) != 0 {
		t.Fatalf("input frame = %x", got)
	}
	records := partyDungeonRecords(got, 0x0027, 11)
	if len(records) != 2 || !bytes.Equal(records[0], input1) || !bytes.Equal(records[1], input2) || partyDungeonFrameContainsCommand(got, 0x0044) {
		t.Fatalf("input records = %x frame=%x", records, got)
	}
	if vo.partyTQOSReliableSeq[0][1] != 1 {
		t.Fatalf("reliable sequence = %d", vo.partyTQOSReliableSeq[0][1])
	}
}

func TestPartyDungeonFrameRecords(t *testing.T) {
	position := mustPartyHex(t, "028703000034000000015100970cfec701070034ede5df0001070034ede5df1102a97492965e0800000d000000ffffffffffffffff0000000000000000")
	if got := partyDungeonFrameRecords(position); got != "0x0051/52" {
		t.Fatalf("position records = %q", got)
	}
	if !partyDungeonFrameContainsCommand(position, 0x0051) || partyDungeonFrameContainsCommand(position, 0x0027) {
		t.Fatalf("position command detection failed")
	}
	reliable := []byte{0x02, 0x44, 0x00, 0xa7, 0xeb, 0x50, 0x2b, 0xec, 0xe8, 0x7e, 0x7e, 0x7e, 0x7e}
	frame := make([]byte, 11+len(reliable))
	frame[0] = 0x01
	binary.LittleEndian.PutUint16(frame[5:7], uint16(2+len(reliable)))
	binary.LittleEndian.PutUint16(frame[9:11], uint16(len(reliable)))
	copy(frame[11:], reliable)
	if got := partyDungeonFrameRecords(frame); got != "0x0044/13" {
		t.Fatalf("reliable records = %q", got)
	}
	if !partyDungeonFrameContainsCommand(frame, 0x0044) || partyDungeonFrameContainsCommand(frame, 0x0051) {
		t.Fatalf("reliable command detection failed")
	}
	if got := partyDungeonRecords(frame, 0x0044, 13); len(got) != 1 || !bytes.Equal(got[0], reliable) {
		t.Fatalf("reliable records = %x", got)
	}
	if got := partyDungeonRecords(position, 0x0051, 0); len(got) != 0 {
		t.Fatal("standalone unreliable body was treated as a reliable record")
	}
}

func TestParsePartyTQOSCapturedPackets(t *testing.T) {
	tests := []struct {
		packet             string
		route, slot, state byte
	}{
		{"02000000000a000000000000b86e32b47e7d7e", 0, 0, 3},
		{"02010000000a000000000000ff4d23cb7e7d7f", 1, 0, 3},
		{"02020000000a000000000000ad0a61d17e7d7c", 2, 0, 3},
		{"02000000000a0001000000009110de907f7d7f", 1, 1, 3},
		{"01070000000a0000000000005fcfd1967e7c7f", 1, 0, 2},
	}
	for _, tt := range tests {
		got, ok := parsePartyTQOSPacket(mustPartyHex(t, tt.packet), tt.route)
		if !ok || got.senderSlot != tt.slot || got.state != tt.state || got.route != tt.route || got.codec != (partyTQOSCodec{key: 0x7e}) {
			t.Fatalf("decoded packet = %+v ok=%v", got, ok)
		}
	}
}

func TestPartyTQOSReliableInnerRecord(t *testing.T) {
	packet := buildPartyTQOSPacket(7, 1, 0, 2, 1, partyTQOSCodec{key: 0x7e})
	packet = append(packet, 0x0b, 0x00)
	packet = append(packet, make([]byte, 11)...)
	binary.LittleEndian.PutUint16(packet[5:7], uint16(len(packet)-9))

	got, ok := parsePartyTQOSPacket(packet, 1)
	if !ok || got.typ != 1 || got.sequence != 7 || got.senderSlot != 1 || got.state != 2 {
		t.Fatalf("packet = %+v ok=%v", got, ok)
	}
	want := "01000000000c0001000a0000000031922ccd7f7c7f"
	if got := hex.EncodeToString(buildPartyTQOSPacket(0, 1, 0, 2, 1, partyTQOSCodec{key: 0x7e})); got != want {
		t.Fatalf("reliable state2 = %s, want %s", got, want)
	}
}

func TestRobotPartyTQOSStateMachine(t *testing.T) {
	vo := &RobotVo{UID: 17000014}
	vo.partySelfPeer = partyIPPeer{slot: 1, slotKnown: true}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 0x2749, accID: 18000000, slot: 0, slotKnown: true, outerIP: net.IPv4(192, 168, 200, 1), port: 5063}
	remote := &net.UDPAddr{IP: net.IPv4(192, 168, 200, 1), Port: 5063}
	inputs := []string{
		"02000000000a000000000000ff4d23cb7e7d7f",
		"02050000000a00000000000085e845b67e7e7f",
		"02060000000a000000000000256ab7eb7e7f7f",
	}
	wants := []string{
		"02000000000a000100000000ebb5b8ed7f7e7f",
		"02010000000a0001000000004b374ab07f7f7f",
		"01000000000c0001000a0000000031922ccd7f7c7f",
	}
	for i := range inputs {
		got := vo.buildPartyUDPAcks(mustPartyHex(t, inputs[i]), remote)
		if len(got) != 1 || hex.EncodeToString(got[0]) != wants[i] {
			t.Fatalf("step %d replies=%x, want %s", i, got, wants[i])
		}
	}
	ack := vo.buildPartyUDPAcks(mustPartyHex(t, "01070000000a0000000000005fcfd1967e7c7f"), remote)
	if len(ack) != 1 || hex.EncodeToString(ack[0]) != "0001080000000000" {
		t.Fatalf("reliable ack = %x", ack)
	}
	if vo.partyTQOSSeq[0][1] != 2 || vo.partyTQOSReliableSeq[0][1] != 1 {
		t.Fatalf("sequences = %d/%d, want 2/1", vo.partyTQOSSeq[0][1], vo.partyTQOSReliableSeq[0][1])
	}
}

func TestRobotPartyTQOSSequencesAreIsolatedByPeerAndRoute(t *testing.T) {
	codec := partyTQOSCodec{key: 0x7e}
	vo := &RobotVo{}
	vo.partySelfPeer = partyIPPeer{slot: 2, slotKnown: true}
	leader := partyIPPeer{uniqueID: 1, accID: 18000000, slot: 0, slotKnown: true}
	robotPeer := partyIPPeer{uniqueID: 2, accID: 17000002, slot: 1, slotKnown: true}
	vo.partyPeers[0] = leader
	vo.partyPeers[1] = robotPeer

	leaderRoute1 := vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(10, leader.slot, 0, 3, 1, codec), 1, leader)
	robotRoute1 := vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(20, robotPeer.slot, 0, 3, 1, codec), 1, robotPeer)
	leaderRoute2 := vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(30, leader.slot, 0, 3, 2, codec), 2, leader)
	for name, replies := range map[string][][]byte{
		"leader route1": leaderRoute1,
		"robot route1":  robotRoute1,
		"leader route2": leaderRoute2,
	} {
		if len(replies) != 1 {
			t.Fatalf("%s replies = %x", name, replies)
		}
		if sequence := binary.LittleEndian.Uint32(replies[0][1:5]); sequence != 0 {
			t.Fatalf("%s sequence = %d, want 0", name, sequence)
		}
	}

	leaderRoute1 = vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(11, leader.slot, 0, 3, 1, codec), 1, leader)
	if len(leaderRoute1) != 1 || binary.LittleEndian.Uint32(leaderRoute1[0][1:5]) != 1 {
		t.Fatalf("leader route1 second replies = %x", leaderRoute1)
	}
	if vo.partyTQOSSeq[0][1] != 2 || vo.partyTQOSSeq[1][1] != 1 || vo.partyTQOSSeq[0][2] != 1 {
		t.Fatalf("isolated sequences = %+v", vo.partyTQOSSeq)
	}
	if !vo.partyRobotPeerReady[1] {
		t.Fatal("robot peer TQOS was not marked ready")
	}
}

func TestPartyPeerUpdateResetsOnlyChangedSlotTransport(t *testing.T) {
	vo := &RobotVo{}
	vo.partySelfPeer = partyIPPeer{uniqueID: 9, slot: 2, slotKnown: true}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 1, slot: 0, slotKnown: true}
	vo.partyTQOSSeq[0][1] = 7
	vo.partyTQOSSeq[1][1] = 5
	vo.partyTQOSCodecKnown[0][1] = true
	vo.partyTQOSCodecKnown[1][1] = true
	vo.partyRobotProbeCount[0] = 1
	vo.partyRobotProbeCount[1] = 1

	vo.setPartyPeersUnsafe([]partyIPPeer{
		{uniqueID: 1, slot: 0, slotKnown: true},
		{uniqueID: 2, slot: 1, slotKnown: true},
	})
	if vo.partyTQOSSeq[0][1] != 7 || !vo.partyTQOSCodecKnown[0][1] || vo.partyRobotProbeCount[0] != 1 {
		t.Fatalf("unchanged leader transport was reset: seq=%d codec=%t probe=%d", vo.partyTQOSSeq[0][1], vo.partyTQOSCodecKnown[0][1], vo.partyRobotProbeCount[0])
	}
	if vo.partyTQOSSeq[1][1] != 0 || vo.partyTQOSCodecKnown[1][1] || vo.partyRobotProbeCount[1] != 0 {
		t.Fatalf("new peer transport was not reset: seq=%d codec=%t probe=%d", vo.partyTQOSSeq[1][1], vo.partyTQOSCodecKnown[1][1], vo.partyRobotProbeCount[1])
	}

	vo.setPartyPeersUnsafe([]partyIPPeer{
		{uniqueID: 3, slot: 0, slotKnown: true},
		{uniqueID: 2, slot: 1, slotKnown: true},
	})
	if vo.partyTQOSSeq[0][1] != 0 || vo.partyTQOSCodecKnown[0][1] {
		t.Fatalf("replaced leader transport was not reset: seq=%d codec=%t", vo.partyTQOSSeq[0][1], vo.partyTQOSCodecKnown[0][1])
	}
}

func TestPartyRobotPeersNegotiateOverDynamicUDP(t *testing.T) {
	receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	sender, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()

	vo := &RobotVo{partyUDPConn: sender}
	vo.partySelfPeer = partyIPPeer{uniqueID: 1, accID: 17000001, slot: 1, slotKnown: true}
	peerAddr := receiver.LocalAddr().(*net.UDPAddr)
	vo.partyPeers[0] = partyIPPeer{uniqueID: 2, accID: 17000002, slot: 2, slotKnown: true, outerIP: peerAddr.IP, port: uint16(peerAddr.Port)}
	if !vo.sendPartyRobotPeerProbeUnsafe(vo.partyPeers[0], 1) {
		t.Fatal("robot peer probe was not sent")
	}

	if err := receiver.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	packet, ok := parsePartyTQOSPacket(buf[:n], 1)
	if !ok || packet.senderSlot != 1 || packet.state != 3 || packet.sequence != 0 {
		t.Fatalf("probe = %+v ok=%v raw=%x", packet, ok, buf[:n])
	}
	if vo.partyTQOSSeq[2][1] != 1 {
		t.Fatalf("probe sequence = %d", vo.partyTQOSSeq[2][1])
	}
}

func TestPartyUDPLoopRetriesRobotPeerProbe(t *testing.T) {
	receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	sender, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}

	vo := &RobotVo{UID: 17000001, State: StateRun, partyUDPConn: sender}
	vo.partySelfPeer = partyIPPeer{uniqueID: 1, accID: 17000001, slot: 1, slotKnown: true}
	peerAddr := receiver.LocalAddr().(*net.UDPAddr)
	vo.partyPeers[0] = partyIPPeer{uniqueID: 2, accID: 17000002, slot: 2, slotKnown: true, outerIP: peerAddr.IP, port: uint16(peerAddr.Port)}
	go vo.partyUDPLoop(sender, vo.UID)
	go vo.partyUDPProbeLoop(sender)
	defer func() {
		vo.mu.Lock()
		vo.State = StateStop
		vo.mu.Unlock()
		_ = sender.Close()
	}()

	if err := receiver.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	for attempt := 1; attempt <= 2; attempt++ {
		n, _, err := receiver.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("probe attempt %d: %v", attempt, err)
		}
		packet, ok := parsePartyTQOSPacket(buf[:n], 1)
		if !ok || packet.state != 3 || packet.sequence != uint32(attempt-1) {
			t.Fatalf("probe attempt %d = %+v ok=%v raw=%x", attempt, packet, ok, buf[:n])
		}
	}
}

func TestPartyRobotPeerNegotiationDoesNotDependOnAccountOrder(t *testing.T) {
	receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	sender, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()

	vo := &RobotVo{partyUDPConn: sender}
	vo.partySelfPeer = partyIPPeer{uniqueID: 2, accID: 17000002, slot: 2, slotKnown: true}
	peerAddr := receiver.LocalAddr().(*net.UDPAddr)
	vo.partyPeers[0] = partyIPPeer{uniqueID: 1, accID: 17000001, slot: 1, slotKnown: true, outerIP: peerAddr.IP, port: uint16(peerAddr.Port)}
	vo.startPartyRobotPeerNegotiationUnsafe()

	if err := receiver.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	packet, ok := parsePartyTQOSPacket(buf[:n], 1)
	if !ok || packet.senderSlot != 2 || packet.state != 3 {
		t.Fatalf("higher-account probe = %+v ok=%v raw=%x", packet, ok, buf[:n])
	}
}

func TestPartyUDPPortFallsBackWhenRequestedPortIsBusy(t *testing.T) {
	busy, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	requested := busy.LocalAddr().(*net.UDPAddr)
	vo := &RobotVo{UID: 17000001}
	if !vo.startPartyUDPUnsafe(&net.TCPAddr{IP: requested.IP, Port: requested.Port}) {
		t.Fatal("fallback UDP listen failed")
	}
	defer vo.closePartyUDPUnsafe()
	actual := vo.partyUDPConn.LocalAddr().(*net.UDPAddr)
	if actual.Port == requested.Port || actual.Port == 0 {
		t.Fatalf("fallback port = %d, requested %d", actual.Port, requested.Port)
	}
}

func TestRobotPartyReliableTransportAckCompositeFrames(t *testing.T) {
	vo := &RobotVo{}
	vo.partySelfPeer = partyIPPeer{slot: 1, slotKnown: true}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 1, slot: 0, slotKnown: true, outerIP: net.IPv4(192, 168, 200, 1), port: 5063}
	remote := &net.UDPAddr{IP: net.IPv4(192, 168, 200, 1), Port: 5063}
	buildFrame := func(seq uint32, bodySize int) []byte {
		frame := make([]byte, 9+bodySize)
		frame[0] = 1
		binary.LittleEndian.PutUint32(frame[1:5], seq)
		binary.LittleEndian.PutUint16(frame[5:7], uint16(bodySize))
		return frame
	}
	payload := append(buildFrame(0x1b2, 39), buildFrame(0x1b3, 52)...)
	got := vo.buildPartyUDPAcks(payload, remote)
	if len(got) != 2 || hex.EncodeToString(got[0]) != "0001b30100000000" || hex.EncodeToString(got[1]) != "0001b40100000000" {
		t.Fatalf("composite ACKs = %x", got)
	}
}

func TestRobotPartyTQOSSelectsPeerBySenderSlot(t *testing.T) {
	vo := &RobotVo{}
	vo.partySelfPeer = partyIPPeer{slot: 2, slotKnown: true}
	endpoint := net.IPv4(192, 168, 200, 1)
	vo.partyPeers[0] = partyIPPeer{uniqueID: 1, slot: 0, slotKnown: true, outerIP: endpoint, port: 5063}
	vo.partyPeers[1] = partyIPPeer{uniqueID: 2, slot: 1, slotKnown: true, outerIP: endpoint, port: 5063}
	remote := &net.UDPAddr{IP: endpoint, Port: 5063}
	replies := vo.buildPartyUDPAcks(mustPartyHex(t, "02000000000a0001000000009110de907f7d7f"), remote)
	if len(replies) != 1 {
		t.Fatalf("replies = %x", replies)
	}
	got, ok := parsePartyTQOSPacket(replies[0], 1)
	if !ok || got.senderSlot != 2 || got.state != 0 {
		t.Fatalf("reply = %+v ok=%v", got, ok)
	}
}

func TestRobotPartyTQOSUsesCachedCodec(t *testing.T) {
	codec := partyTQOSCodec{key: 0xa5, rotate: 3}
	vo := &RobotVo{}
	vo.partySelfPeer = partyIPPeer{slot: 0, slotKnown: true}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 1, slot: 1, slotKnown: true, outerIP: net.IPv4(192, 168, 200, 1), port: 5063}
	remote := &net.UDPAddr{IP: net.IPv4(192, 168, 200, 1), Port: 5063}
	if got := vo.buildPartyUDPAcks(buildPartyTQOSPacket(7, 1, 0, 3, 1, codec), remote); len(got) != 1 {
		t.Fatalf("state3 replies = %x", got)
	}
	replies := vo.buildPartyUDPAcks(buildPartyTQOSPacket(8, 1, 0, 1, 1, codec), remote)
	if len(replies) != 1 {
		t.Fatalf("state1 replies = %x", replies)
	}
	got, ok := parsePartyTQOSPacketWithCodec(replies[0], 1, &codec)
	if !ok || got.typ != 1 || got.state != 2 || got.codec != codec {
		t.Fatalf("state2 = %+v ok=%v", got, ok)
	}
}

func TestShouldReplyPartyUDP(t *testing.T) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	local := conn.LocalAddr().(*net.UDPAddr)
	if shouldReplyPartyUDP(conn, local) {
		t.Fatal("same socket should be treated as a self-loop")
	}
	if !shouldReplyPartyUDP(conn, &net.UDPAddr{IP: local.IP, Port: local.Port + 1}) {
		t.Fatal("same IP with another port should support robot-to-robot party")
	}
}

func TestBuildPartyRelayPacket(t *testing.T) {
	got := buildPartyRelayPacket(1, 18000000, 17000006, []byte{1, 2, 3})
	want := []byte{1, 0, 15, 0, 0x80, 0xa8, 0x12, 0x01, 0x46, 0x66, 0x03, 0x01, 1, 2, 3}
	if !bytes.Equal(got, want) {
		t.Fatalf("packet = %x, want %x", got, want)
	}
}

func TestPartyRelayBadPacketClearsCurrentConnection(t *testing.T) {
	robotConn, peerConn := net.Pipe()
	defer peerConn.Close()
	vo := &RobotVo{UID: 7, State: StateRun, partyRelayConn: robotConn}
	done := make(chan struct{})
	go func() {
		vo.partyRelayLoop(robotConn, vo.UID)
		close(done)
	}()

	packet := make([]byte, 12)
	binary.LittleEndian.PutUint16(packet[2:4], 11)
	if _, err := peerConn.Write(packet); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("relay loop did not stop after a malformed packet")
	}
	vo.mu.Lock()
	defer vo.mu.Unlock()
	if vo.partyRelayConn != nil {
		t.Fatal("malformed relay packet left a stale active connection")
	}
}

func TestPartyRelayNormalCloseIsAlreadyDetached(t *testing.T) {
	robotConn, peerConn := net.Pipe()
	defer peerConn.Close()
	vo := &RobotVo{State: StateRun, partyRelayConn: robotConn}

	vo.mu.Lock()
	vo.closePartyRelayUnsafe()
	vo.mu.Unlock()
	if vo.detachPartyRelayConn(robotConn) {
		t.Fatal("normal relay close was classified as an unexpected disconnect")
	}
	vo.mu.Lock()
	defer vo.mu.Unlock()
	if vo.partyRelayConn != nil {
		t.Fatal("normal relay close left a stale active connection")
	}
}

func putPartyPeer(dst []byte, uniqueID uint16, ip net.IP, port uint16, accID uint32, natType byte, mtu uint32) {
	binary.LittleEndian.PutUint16(dst[:2], uniqueID)
	copy(dst[2:6], ip.To4())
	copy(dst[6:10], ip.To4())
	binary.BigEndian.PutUint16(dst[10:12], port)
	binary.LittleEndian.PutUint32(dst[12:16], accID)
	dst[16] = natType
	binary.LittleEndian.PutUint32(dst[17:21], mtu)
}

func mustPartyHex(t *testing.T, value string) []byte {
	t.Helper()
	data, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("decode %q: %v", value, err)
	}
	return data
}
