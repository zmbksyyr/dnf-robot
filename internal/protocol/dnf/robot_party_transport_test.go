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
	if !vo.ensurePartyUDPLoopUnsafe() {
		t.Fatal("active party UDP loop did not start")
	}
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

func TestPartyRobotPeerProbeRestartsAfterCooldown(t *testing.T) {
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
	vo.partyRobotProbeCount[2] = 4
	vo.partyRobotProbeAt[2] = time.Now().Add(-partyRobotProbeCooldown - time.Second)
	vo.startPartyRobotPeerNegotiationUnsafe()

	if err := receiver.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	if _, _, err := receiver.ReadFromUDP(buf); err != nil {
		t.Fatal(err)
	}
	if vo.partyRobotProbeCount[2] != 1 {
		t.Fatalf("probe count after cooldown = %d", vo.partyRobotProbeCount[2])
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

func TestPartyUDPLoopRunsOnlyWhilePartyActive(t *testing.T) {
	vo := &RobotVo{UID: 17000001, State: StateRun}
	vo.mu.Lock()
	if !vo.startPartyUDPUnsafe(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}) {
		vo.mu.Unlock()
		t.Fatal("UDP bind failed")
	}
	conn := vo.partyUDPConn
	if vo.partyUDPRunning {
		vo.mu.Unlock()
		t.Fatal("idle robot started a party UDP loop")
	}
	vo.setPartyPendingUnsafe(7)
	if !vo.partyUDPRunning {
		vo.mu.Unlock()
		t.Fatal("party pending did not start the UDP loop")
	}
	vo.clearPartyUnsafe()
	vo.mu.Unlock()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		vo.mu.Lock()
		running := vo.partyUDPRunning
		bound := vo.partyUDPConn == conn
		vo.mu.Unlock()
		if !running {
			if !bound {
				t.Fatal("party clear closed the bound NAT socket")
			}
			vo.mu.Lock()
			vo.closePartyUDPUnsafe()
			vo.mu.Unlock()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("party UDP loop did not stop after clear")
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

func TestPartyRouteFallsBackWhenCachedTransportIsUnavailable(t *testing.T) {
	udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer udpConn.Close()
	vo := &RobotVo{partyUDPConn: udpConn}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 1, accID: 18000000, slot: 0, slotKnown: true, outerIP: net.IPv4(127, 0, 0, 1), port: 5063}
	vo.partyPeerRoute[0] = 2
	vo.partyPeerRouteAt[0] = time.Now()
	if route := vo.partyRouteForPeerUnsafe(0); route != 1 {
		t.Fatalf("route with unavailable relay = %d", route)
	}

	relay, peerConn := net.Pipe()
	defer relay.Close()
	defer peerConn.Close()
	vo.partyUDPConn = nil
	vo.partyRelayConn = relay
	vo.partyPeerRoute[0] = 1
	if route := vo.partyRouteForPeerUnsafe(0); route != 2 {
		t.Fatalf("route with unavailable UDP = %d", route)
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

func TestPartyRelayQueuedWriteDoesNotHoldRobotMutex(t *testing.T) {
	robotConn, peerConn := net.Pipe()
	defer peerConn.Close()
	vo := &RobotVo{UID: 17000001, State: StateRun, partyRelayConn: robotConn}
	peer := partyIPPeer{uniqueID: 7, accID: 18000000, slot: 0, slotKnown: true}

	vo.mu.Lock()
	started := time.Now()
	_, err := vo.sendPartyTransportUnsafe(nil, peer, 2, []byte("blocked relay write"))
	vo.mu.Unlock()
	if err != nil {
		t.Fatalf("queue relay write: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("relay enqueue blocked for %s", elapsed)
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
		t.Fatal("blocked relay socket held the Robot mutex")
	}

	vo.mu.Lock()
	vo.closePartyRelayUnsafe()
	vo.mu.Unlock()
}

func TestPartyRelayWriteFailureDetachesAndFallsBackToUDP(t *testing.T) {
	robotConn, peerConn := net.Pipe()
	_ = peerConn.Close()
	udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer udpConn.Close()

	vo := &RobotVo{UID: 17000001, State: StateRun, partyRelayConn: robotConn, partyUDPConn: udpConn}
	peer := partyIPPeer{uniqueID: 7, accID: 18000000, slot: 0, slotKnown: true, outerIP: net.IPv4(127, 0, 0, 1), port: 5063}
	vo.partyPeers[0] = peer
	vo.partyPeerRoute[0] = 2
	vo.partyPeerRouteAt[0] = time.Now()

	vo.mu.Lock()
	_, err = vo.sendPartyTransportUnsafe(nil, peer, 2, []byte("write failure"))
	vo.mu.Unlock()
	if err != nil {
		t.Fatalf("queue relay write: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for {
		vo.mu.Lock()
		detached := vo.partyRelayConn == nil && vo.partyRelayWriter == nil
		route := vo.partyRouteForPeerUnsafe(0)
		vo.mu.Unlock()
		if detached {
			if route != 1 {
				t.Fatalf("route after relay failure=%d want UDP", route)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("relay write failure left a stale connection")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestPartyRelayWriteQueueIsBounded(t *testing.T) {
	robotConn, peerConn := net.Pipe()
	defer peerConn.Close()
	vo := &RobotVo{UID: 17000001, State: StateRun, partyRelayConn: robotConn}
	peer := partyIPPeer{uniqueID: 7, accID: 18000000, slot: 0, slotKnown: true}

	started := time.Now()
	var queueErr error
	vo.mu.Lock()
	for i := 0; i < partyRelayWriteQueueSize+2; i++ {
		if _, err := vo.sendPartyTransportUnsafe(nil, peer, 2, []byte{byte(i)}); err != nil {
			queueErr = err
			break
		}
	}
	detached := vo.partyRelayConn == nil && vo.partyRelayWriter == nil
	vo.mu.Unlock()
	if queueErr == nil {
		t.Fatal("relay queue accepted an unbounded write burst")
	}
	if !detached {
		t.Fatal("relay queue overflow did not detach the connection")
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("relay queue overflow blocked for %s", elapsed)
	}
}
