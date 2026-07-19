package dnf

import (
	"bytes"
	"encoding/binary"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

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

func TestEnsurePartyRelayIsAsyncSingleflightAndUsesLoginIP(t *testing.T) {
	gameConn, gamePeer := net.Pipe()
	defer gameConn.Close()
	defer gamePeer.Close()

	release := make(chan struct{})
	address := make(chan string, 1)
	var calls atomic.Int32
	vo := &RobotVo{
		UID:           17000001,
		LoginIP:       "192.0.2.44",
		State:         StateRun,
		Conn:          gameConn,
		partySelfPeer: partyIPPeer{uniqueID: 7, accID: 17000001, slot: 1, slotKnown: true},
		partyRelayDial: func(_, target string, _ time.Duration) (net.Conn, error) {
			calls.Add(1)
			address <- target
			<-release
			return nil, net.ErrClosed
		},
	}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 9, accID: 18000000, slot: 0, slotKnown: true}

	vo.mu.Lock()
	vo.ensurePartyRelayUnsafe()
	vo.ensurePartyRelayUnsafe()
	vo.mu.Unlock()

	select {
	case got := <-address:
		want := net.JoinHostPort(vo.LoginIP, "7200")
		if got != want {
			t.Fatalf("relay address=%q want=%q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("relay dial did not start")
	}
	locked := make(chan struct{})
	go func() {
		vo.mu.Lock()
		vo.mu.Unlock()
		close(locked)
	}()
	select {
	case <-locked:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("relay dial held the Robot mutex")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("dial calls=%d want=1", got)
	}
	close(release)
}
