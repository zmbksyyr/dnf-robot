package dnf

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func TestPartyGeneratedReliableFrameRetransmitsUntilACK(t *testing.T) {
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

	now := time.Unix(1000, 0)
	remote := receiver.LocalAddr().(*net.UDPAddr)
	peer := partyIPPeer{uniqueID: 0x1111, accID: 18000000, slot: 0, slotKnown: true, outerIP: remote.IP, port: uint16(remote.Port)}
	vo := &RobotVo{UID: 17000001, partyUDPConn: sender, partyUDPRunning: true}
	vo.partySelfPeer = partyIPPeer{uniqueID: 0x2222, accID: 17000001, slot: 1, slotKnown: true}
	vo.partyPeers[0] = peer
	if _, err := vo.sendPartyReliableUnsafe(sender, peer, 5, [][]byte{{1, 2, 3}}, "test", now); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 128)
	_ = receiver.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	first := append([]byte(nil), buf[:n]...)
	vo.flushPartyReliableUnsafe(sender, now.Add(partyReliableRetryInitial))
	n, _, err = receiver.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, buf[:n]) || binary.LittleEndian.Uint32(first[1:5]) != 0 || vo.partyTQOSReliableSeq[0][1] != 1 {
		t.Fatalf("retry changed reliable frame first=%x retry=%x next=%d", first, buf[:n], vo.partyTQOSReliableSeq[0][1])
	}

	vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSAck(peer.slot, 0), 1, peer)
	if len(vo.partyReliablePending[0][1]) != 0 {
		t.Fatalf("matching ACK did not drain pending frame: %+v", vo.partyReliablePending[0][1])
	}
}

func TestPartyGeneratedReliableStallReopensTQOSEpoch(t *testing.T) {
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

	now := time.Unix(2000, 0)
	remote := receiver.LocalAddr().(*net.UDPAddr)
	peer := partyIPPeer{uniqueID: 0x1111, accID: 18000000, slot: 0, slotKnown: true, outerIP: remote.IP, port: uint16(remote.Port)}
	vo := &RobotVo{UID: 17000001, partyUDPConn: sender, partyUDPRunning: true}
	vo.partySelfPeer = partyIPPeer{uniqueID: 0x2222, accID: 17000001, slot: 1, slotKnown: true}
	vo.partyPeers[0] = peer
	if _, err := vo.sendPartyReliableUnsafe(sender, peer, 5, [][]byte{{1, 2, 3}}, "skill-cast", now); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 128)
	_ = receiver.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := receiver.ReadFromUDP(buf); err != nil {
		t.Fatal(err)
	}
	vo.partyReliablePending[0][1][0].firstSentAt = now.Add(-partyReliableRecoverAfter)
	vo.flushPartyReliableUnsafe(sender, now)
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	recovery, ok := parsePartyTQOSPacket(buf[:n], 1)
	if !ok || recovery.state != 3 || recovery.sequence != 0 {
		t.Fatalf("route recovery frame=%+v ok=%t raw=%x", recovery, ok, buf[:n])
	}
	if len(vo.partyReliablePending[0][1]) != 0 || vo.partyTQOSReliableSeq[0][1] != 0 || vo.partyTQOSSeq[0][1] != 1 {
		t.Fatalf("route epoch was not reset pending=%d reliable=%d unreliable=%d", len(vo.partyReliablePending[0][1]), vo.partyTQOSReliableSeq[0][1], vo.partyTQOSSeq[0][1])
	}
	if vo.partySkillBlockedUntil != now.Add(partySkillFailureCooldown) {
		t.Fatalf("skill circuit breaker=%s", vo.partySkillBlockedUntil)
	}
}

func TestPartyDungeonSkillWaitsForSelfUniqueID(t *testing.T) {
	vo := &RobotVo{UID: 17000001}
	vo.partySelfPeer = partyIPPeer{accID: 17000001, slot: 1, slotKnown: true}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 0x1111, accID: 18000000, slot: 0, slotKnown: true}
	if vo.sendPartySkillStateUnsafe(nil, time.Now(), 22, []byte{3, 0, 0}, "TEST") {
		t.Fatal("dungeon state was emitted with unique_id=0")
	}
}

func TestPartyRouteFailureKeepsReadyAlternateRoute(t *testing.T) {
	peer := partyIPPeer{uniqueID: 0x1111, accID: 17000002, slot: 2, slotKnown: true, outerIP: net.IPv4(127, 0, 0, 1), port: 5063}
	relayRobot, relayPeer := net.Pipe()
	defer relayRobot.Close()
	defer relayPeer.Close()
	vo := &RobotVo{UID: 17000001, partyUDPRunning: true, partyRelayConn: relayRobot}
	vo.partyPeers[0] = peer
	vo.partyRobotRouteReady[peer.slot][1] = true
	vo.partyRobotRouteReady[peer.slot][2] = true
	vo.partyRobotPeerReady[peer.slot] = true
	vo.partyPeerRoute[peer.slot] = 1
	vo.partyPeerRouteAt[peer.slot] = time.Now()

	vo.markPartyRouteFailureUnsafe(peer, 1, time.Now(), "test route failure")
	if vo.partyRobotRouteReady[peer.slot][1] || !vo.partyRobotRouteReady[peer.slot][2] || !vo.partyRobotPeerReady[peer.slot] {
		t.Fatalf("alternate route readiness was lost: route=%v peer=%t", vo.partyRobotRouteReady[peer.slot], vo.partyRobotPeerReady[peer.slot])
	}
	if route := vo.partyRouteForPeerUnsafe(peer.slot); route != 2 {
		t.Fatalf("route=%d want relay alternate", route)
	}
	if vo.partyPeerRoute[peer.slot] != 2 {
		t.Fatalf("remembered route=%d want relay alternate", vo.partyPeerRoute[peer.slot])
	}
}

func TestPartyReadyRouteHeartbeatReusesState2Sequence(t *testing.T) {
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

	now := time.Unix(3000, 0)
	remote := receiver.LocalAddr().(*net.UDPAddr)
	peer := partyIPPeer{uniqueID: 0x1111, accID: 17000002, slot: 2, slotKnown: true, outerIP: remote.IP, port: uint16(remote.Port)}
	packet := buildPartyTQOSPacket(7, 1, 0, 2, 1, partyTQOSCodec{key: 0x7e})
	vo := &RobotVo{UID: 17000001, partyUDPConn: sender, partyUDPRunning: true}
	vo.partySelfPeer = partyIPPeer{uniqueID: 0x2222, accID: 17000001, slot: 1, slotKnown: true}
	vo.partyPeers[0] = peer
	vo.partyRobotRouteReady[peer.slot][1] = true
	vo.partyRobotPeerReady[peer.slot] = true
	vo.partyRouteActivityAt[peer.slot][1] = now.Add(-partyRobotHealthInterval)
	vo.partyTQOSReliableSeq[peer.slot][1] = 8
	vo.partyTQOSReplies[peer.slot][1] = partyTQOSReliableReply{packet: packet, acknowledged: true}

	vo.probePartyRobotPeerHealthUnsafe(sender, now)
	buf := make([]byte, 128)
	_ = receiver.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf[:n], packet) || vo.partyTQOSReliableSeq[peer.slot][1] != 8 {
		t.Fatalf("heartbeat changed state2 packet=%x sequence=%d", buf[:n], vo.partyTQOSReliableSeq[peer.slot][1])
	}
	pending := vo.partyTQOSReplies[peer.slot][1]
	if pending.acknowledged || pending.retries != 0 || pending.nextRetry != now.Add(partyTQOSRetryInterval) {
		t.Fatalf("heartbeat pending=%+v", pending)
	}
}
