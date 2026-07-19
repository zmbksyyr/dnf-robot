package dnf

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

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

func TestSetPartyPeersKeepsAccountOnlyRobotPeers(t *testing.T) {
	vo := &RobotVo{State: StateRun}
	vo.setPartyPeersUnsafe([]partyIPPeer{
		{uniqueID: 7, accID: 18000000, slot: 0, slotKnown: true},
		{accID: 17000026, slot: 2, slotKnown: true},
	})

	peer := vo.partyPeerForSlotUnsafe(2)
	if peer.accID != 17000026 || !peer.slotKnown || peer.slot != 2 {
		t.Fatalf("account-only peer was not retained: %+v", peer)
	}
}

func TestAccountOnlyPeerKeepsPartyLifecycleActive(t *testing.T) {
	vo := &RobotVo{State: StateRun}
	vo.partyPeers[0] = partyIPPeer{accID: 17000026, slot: 2, slotKnown: true}
	if !vo.partyActiveUnsafe() {
		t.Fatal("account-only confirmed peer was treated as an empty party slot")
	}
}

func TestPartySelfSnapshotPreservesKnownUniqueID(t *testing.T) {
	vo := &RobotVo{UID: 17000026}
	vo.partySelfPeer = partyIPPeer{uniqueID: 0x2244, accID: 17000026, slot: 2, slotKnown: true, port: 45000}
	vo.setPartySelfPeerUnsafe(partyIPPeer{accID: 17000026, slot: 2, slotKnown: true, port: 46000})
	if vo.partySelfPeer.uniqueID != 0x2244 || vo.partySelfPeer.port != 46000 {
		t.Fatalf("partial self snapshot lost identity or endpoint: %+v", vo.partySelfPeer)
	}
}

func TestPartySelfSnapshotMergesAnonymousEndpointUpdate(t *testing.T) {
	vo := &RobotVo{UID: 17000026}
	vo.partySelfPeer = partyIPPeer{uniqueID: 0x2244, accID: 17000026, slot: 2, slotKnown: true, port: 45000}
	vo.setPartySelfPeerUnsafe(partyIPPeer{slot: 3, slotKnown: true, port: 46000})
	if vo.partySelfPeer.uniqueID != 0x2244 || vo.partySelfPeer.accID != 17000026 || vo.partySelfPeer.slot != 3 || vo.partySelfPeer.port != 46000 {
		t.Fatalf("anonymous partial self snapshot was not merged: %+v", vo.partySelfPeer)
	}
}

func TestPartyInfoClearStateResetsFollowState(t *testing.T) {
	vo := &RobotVo{}
	vo.partySelfPeer = partyIPPeer{uniqueID: 9, slot: 1, slotKnown: true}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 7, slot: 0, slotKnown: true}
	vo.townEntityPositions = map[uint16]townEntityPosition{7: {uniqueID: 7}}

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
	if vo.partyActiveUnsafe() || len(vo.townEntityPositions) != 0 {
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

func TestPartyPeerAdvertisedEndpointChangeResetsTransport(t *testing.T) {
	oldOuter := net.IPv4(192, 168, 200, 1)
	observed := net.IPv4(192, 168, 200, 2)
	vo := &RobotVo{}
	vo.partySelfPeer = partyIPPeer{uniqueID: 9, slot: 1, slotKnown: true}
	vo.partyPeers[0] = partyIPPeer{
		uniqueID:     1,
		slot:         0,
		slotKnown:    true,
		outerIP:      oldOuter,
		port:         5063,
		observedIP:   observed,
		observedPort: 62000,
	}
	vo.partyTQOSSeq[0][1] = 7
	vo.partyTQOSReceived[0][1] = partyTQOSReceiveWindow{latest: 9, seen: 1, initialized: true}
	vo.partyTQOSReplies[0][1] = partyTQOSReliableReply{packet: []byte{1}}
	vo.partyTQOSCodecKnown[0][1] = true

	newOuter := net.IPv4(192, 168, 200, 3)
	vo.setPartyPeersUnsafe([]partyIPPeer{{
		uniqueID:  1,
		slot:      0,
		slotKnown: true,
		outerIP:   newOuter,
		port:      5064,
	}})

	peer := vo.partyPeerForSlotUnsafe(0)
	if !peer.outerIP.Equal(newOuter) || peer.port != 5064 {
		t.Fatalf("advertised endpoint = %s:%d", peer.outerIP, peer.port)
	}
	if peer.observedIP != nil || peer.observedPort != 0 {
		t.Fatalf("stale observed endpoint was retained: %s:%d", peer.observedIP, peer.observedPort)
	}
	if vo.partyTQOSSeq[0][1] != 0 || vo.partyTQOSReceived[0][1].initialized || len(vo.partyTQOSReplies[0][1].packet) != 0 || vo.partyTQOSCodecKnown[0][1] {
		t.Fatalf("changed endpoint retained TQOS state: seq=%d window=%+v pending=%+v codec=%t",
			vo.partyTQOSSeq[0][1], vo.partyTQOSReceived[0][1], vo.partyTQOSReplies[0][1], vo.partyTQOSCodecKnown[0][1])
	}
}

func TestRepeatedPartyInvitePreservesConfirmedTransport(t *testing.T) {
	robotConn, peerConn := net.Pipe()
	defer robotConn.Close()
	defer peerConn.Close()
	vo := &RobotVo{
		UID:                 17000001,
		State:               StateRun,
		Conn:                robotConn,
		Cipher:              newPartyTestCipher(t),
		townEntityPositions: map[uint16]townEntityPosition{7: {uniqueID: 7}},
	}
	vo.partySelfPeer = partyIPPeer{uniqueID: 9, slot: 1, slotKnown: true}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 1, slot: 0, slotKnown: true}
	vo.partyTQOSSeq[0][1] = 7
	vo.partyTQOSReliableSeq[0][1] = 5
	vo.partyTQOSCodecKnown[0][1] = true
	vo.partyPeerRoute[0] = 1

	readDone := make(chan error, 1)
	go func() {
		response := make([]byte, 21)
		_, err := io.ReadFull(peerConn, response)
		readDone <- err
	}()
	body := make([]byte, 8)
	binary.LittleEndian.PutUint16(body[:2], 7)
	body[2] = peerRequestParty
	binary.LittleEndian.PutUint32(body[3:7], 99)
	vo.parsePacket(makePartyRecvPacket(7, body))
	if err := <-readDone; err != nil {
		t.Fatal(err)
	}
	if vo.partyTQOSSeq[0][1] != 7 || vo.partyTQOSReliableSeq[0][1] != 5 || !vo.partyTQOSCodecKnown[0][1] || vo.partyPeerRoute[0] != 1 {
		t.Fatalf("confirmed transport was reset: seq=%d reliable=%d codec=%t route=%d", vo.partyTQOSSeq[0][1], vo.partyTQOSReliableSeq[0][1], vo.partyTQOSCodecKnown[0][1], vo.partyPeerRoute[0])
	}
	if vo.partyRecvSource != recvBodySourcePlain {
		t.Fatalf("party receive source = %s", vo.partyRecvSource)
	}
}

func TestPartyUDPAuthenticatesBeforeLearningNATEndpoint(t *testing.T) {
	sharedIP := net.IPv4(192, 168, 200, 1)
	vo := &RobotVo{partySelfPeer: partyIPPeer{uniqueID: 3, accID: 17000001, slot: 2, slotKnown: true}}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 1, accID: 18000000, slot: 0, slotKnown: true, innerIP: sharedIP, outerIP: sharedIP, port: 5063}
	vo.partyPeers[1] = partyIPPeer{uniqueID: 2, accID: 18000001, slot: 1, slotKnown: true, innerIP: sharedIP, outerIP: sharedIP, port: 5064}

	remote := &net.UDPAddr{IP: sharedIP, Port: 62000}
	payload := buildPartyTQOSPacket(7, 1, 0, 3, 1, partyTQOSCodec{key: 0x7e})
	if replies := vo.buildPartyUDPAcks(payload, remote); len(replies) == 0 {
		t.Fatal("authenticated NAT endpoint produced no reply")
	}
	if vo.partyPeers[0].observedPort != 0 || vo.partyPeers[1].observedPort != 62000 || !vo.partyPeers[1].observedIP.Equal(sharedIP) {
		t.Fatalf("observed endpoints=%+v", vo.partyPeers)
	}
	advertised := &net.UDPAddr{IP: sharedIP, Port: 5064}
	if replies := vo.buildPartyUDPAcks(buildPartyTQOSPacket(8, 1, 0, 3, 1, partyTQOSCodec{key: 0x7e}), advertised); len(replies) == 0 {
		t.Fatal("advertised endpoint produced no reply")
	}
	if vo.partyPeers[1].observedPort != 5064 || !vo.partyPeers[1].observedIP.Equal(sharedIP) {
		t.Fatalf("advertised endpoint did not refresh observed endpoint: %+v", vo.partyPeers[1])
	}

	unknown := &net.UDPAddr{IP: net.IPv4(203, 0, 113, 9), Port: 62001}
	if replies := vo.buildPartyUDPAcks(payload, unknown); len(replies) != 0 {
		t.Fatalf("unknown IP was accepted: %x", replies)
	}
	if vo.partyPeers[1].observedPort != 5064 {
		t.Fatalf("unknown IP rewrote observed endpoint: %+v", vo.partyPeers[1])
	}

	badSlot := buildPartyTQOSPacket(8, 3, 0, 3, 1, partyTQOSCodec{key: 0x7e})
	if replies := vo.buildPartyUDPAcks(badSlot, &net.UDPAddr{IP: sharedIP, Port: 62002}); len(replies) != 0 {
		t.Fatalf("unknown sender slot was accepted: %x", replies)
	}
}
