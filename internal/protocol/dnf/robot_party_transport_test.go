package dnf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"robot/internal/protocol/dnf/crypt"
)

type panicLocalAddrConn struct {
	net.Conn
}

func (panicLocalAddrConn) LocalAddr() net.Addr {
	panic("test local address panic")
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
	if !vo.sendPartyRobotPeerProbeRouteUnsafe(vo.partyPeers[0], 1, 1) {
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

func TestPartyUDPReadErrorsUseBoundedRetryBackoff(t *testing.T) {
	if !partyUDPReadErrorTerminal(net.ErrClosed) {
		t.Fatal("closed UDP socket was not terminal")
	}
	if partyUDPReadErrorTerminal(errors.New("transient UDP receive failure")) {
		t.Fatal("transient UDP error was treated as terminal")
	}

	want := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond, 80 * time.Millisecond}
	backoff := time.Duration(0)
	for i, delay := range want {
		backoff = nextPartyUDPReadBackoff(backoff)
		if backoff != delay {
			t.Fatalf("backoff[%d] = %s, want %s", i, backoff, delay)
		}
	}
	backoff = partyUDPReadBackoffMax
	if got := nextPartyUDPReadBackoff(backoff); got != partyUDPReadBackoffMax {
		t.Fatalf("max backoff = %s, want %s", got, partyUDPReadBackoffMax)
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
	vo.mu.Lock()
	vo.ensurePartySupervisorUnsafe()
	vo.mu.Unlock()
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

	vo := &RobotVo{partyUDPConn: sender, partyUDPRunning: true}
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

func TestPartyPeerForUDPAcceptsAccountOnlyRobotPeer(t *testing.T) {
	vo := &RobotVo{}
	remote := &net.UDPAddr{IP: net.IPv4(192, 168, 200, 131), Port: 45678}
	vo.partyPeers[2] = partyIPPeer{
		accID:     17000026,
		slot:      2,
		slotKnown: true,
		outerIP:   remote.IP,
		port:      uint16(remote.Port),
	}

	slot := byte(2)
	peer, ok := vo.partyPeerForUDPUnsafe(remote, &slot)
	if !ok || peer.accID != 17000026 || peer.slot != 2 {
		t.Fatalf("peer=%+v ok=%t", peer, ok)
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

	vo := &RobotVo{partyUDPConn: sender, partyUDPRunning: true}
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

func TestPartyTransportReplyGroupingRespectsDatagramLimit(t *testing.T) {
	frames := [][]byte{{1, 2, 3, 4}, {5, 6, 7, 8}, {9, 10, 11, 12}}
	groups := groupPartyTransportFrames(frames, 8)
	if len(groups) != 2 || !bytes.Equal(groups[0], []byte{1, 2, 3, 4, 5, 6, 7, 8}) || !bytes.Equal(groups[1], []byte{9, 10, 11, 12}) {
		t.Fatalf("reply groups=%x", groups)
	}
}

func TestPartyRouteFallsBackWhenCachedTransportIsUnavailable(t *testing.T) {
	udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer udpConn.Close()
	vo := &RobotVo{partyUDPConn: udpConn, partyUDPRunning: true}
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

func TestPartyRelayDisconnectDoesNotDegradeUnusedRoute(t *testing.T) {
	robotConn, peerConn := net.Pipe()
	defer peerConn.Close()
	vo := &RobotVo{State: StateRun, partyRelayConn: robotConn}
	peer := partyIPPeer{uniqueID: 7, accID: 17000002, slot: 1, slotKnown: true}
	vo.partySelfPeer = partyIPPeer{uniqueID: 9, accID: 17000001, slot: 2, slotKnown: true}
	vo.partyPeers[0] = peer

	if !vo.detachPartyRelayConn(robotConn) {
		t.Fatal("active relay disconnect was not classified as unexpected")
	}
	if vo.partyRouteFailures[peer.slot][2] != 0 || !vo.partyRouteBlockedUntil[peer.slot][2].IsZero() {
		t.Fatalf("unused relay route was degraded: failures=%d blocked_until=%s",
			vo.partyRouteFailures[peer.slot][2], vo.partyRouteBlockedUntil[peer.slot][2])
	}
}

func TestPartySelfIdentityStallRebuildsUDPWithoutRelay(t *testing.T) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan *net.TCPConn, 1)
	go func() {
		conn, _ := listener.AcceptTCP()
		accepted <- conn
	}()
	gameConn, err := net.DialTCP("tcp4", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer gameConn.Close()
	gamePeer := <-accepted
	if gamePeer == nil {
		t.Fatal("game listener did not accept connection")
	}
	defer gamePeer.Close()

	oldUDP, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	vo := NewRobotVo(nil)
	vo.UID = 17000001
	vo.State = StateRun
	vo.Conn = gameConn
	vo.Cipher = crypt.NewDNFCipher()
	if err := vo.Cipher.Initialize(make([]byte, 334)); err != nil {
		t.Fatal(err)
	}
	vo.partyUDPConn = oldUDP
	vo.partySelfPeer = partyIPPeer{accID: vo.UID, slot: 1, slotKnown: true}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 7, accID: 17000002, slot: 0, slotKnown: true}
	vo.partySelfRefreshAttempts = partySelfRefreshRecycleAfter
	vo.partySelfRefreshBackoff = partySelfRefreshMax
	t.Cleanup(func() {
		vo.mu.Lock()
		vo.closePartyUDPUnsafe()
		vo.mu.Unlock()
	})

	now := time.Now()
	vo.refreshPartySelfIdentityUnsafe(now)

	if vo.partyUDPConn == nil || vo.partyUDPConn == oldUDP {
		t.Fatal("stalled self identity did not replace the UDP socket")
	}
	if vo.partyRelayConn != nil {
		t.Fatal("test unexpectedly acquired a relay connection")
	}
	if !vo.natInfoSent || vo.PacketID != 1 {
		t.Fatalf("rebuilt UDP did not send NAT info: sent=%t packet_id=%d", vo.natInfoSent, vo.PacketID)
	}
	if vo.partySelfRefreshAttempts != 1 {
		t.Fatalf("refresh attempts=%d want 1 after recycle", vo.partySelfRefreshAttempts)
	}
	if !vo.partySelfRefreshAt.After(now) {
		t.Fatalf("next self refresh=%s want after %s", vo.partySelfRefreshAt, now)
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
	locked := make(chan bool, 1)
	go func() {
		vo.mu.Lock()
		connecting := vo.partyRelayConnecting
		vo.mu.Unlock()
		locked <- connecting
	}()
	select {
	case connecting := <-locked:
		if !connecting {
			t.Fatal("relay connection attempt stopped unexpectedly")
		}
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

	locked := make(chan bool, 1)
	go func() {
		vo.mu.Lock()
		active := vo.partyRelayConn == robotConn
		vo.mu.Unlock()
		locked <- active
	}()
	select {
	case active := <-locked:
		if !active {
			t.Fatal("relay connection detached before the blocked write")
		}
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

	vo := &RobotVo{UID: 17000001, State: StateRun, partyRelayConn: robotConn, partyUDPConn: udpConn, partyUDPRunning: true}
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

func TestPartyRouteFallsBackToRelayWhenUDPLoopStopped(t *testing.T) {
	udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer udpConn.Close()
	relay, peerConn := net.Pipe()
	defer relay.Close()
	defer peerConn.Close()

	vo := &RobotVo{partyUDPConn: udpConn, partyRelayConn: relay}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 1, accID: 18000000, slot: 0, slotKnown: true, outerIP: net.IPv4(127, 0, 0, 1), port: 5063}
	vo.partyPeerRoute[0] = 1
	vo.partyPeerRouteAt[0] = time.Now()

	if route := vo.partyRouteForPeerUnsafe(0); route != 2 {
		t.Fatalf("route with stopped UDP loop = %d, want relay", route)
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

func TestPartySupervisorReconnectsRelayAfterFailure(t *testing.T) {
	gameConn, gamePeer := net.Pipe()
	defer gamePeer.Close()
	var calls atomic.Int32
	connectedPeer := make(chan net.Conn, 1)
	vo := &RobotVo{
		UID:           17000001,
		LoginIP:       "192.0.2.44",
		State:         StateRun,
		Conn:          gameConn,
		partySelfPeer: partyIPPeer{uniqueID: 7, accID: 17000001, slot: 1, slotKnown: true},
		partyRelayDial: func(_, _ string, _ time.Duration) (net.Conn, error) {
			if calls.Add(1) == 1 {
				return nil, errors.New("temporary relay failure")
			}
			robot, peer := net.Pipe()
			go func() {
				auth := make([]byte, 12)
				_, _ = io.ReadFull(peer, auth)
				connectedPeer <- peer
			}()
			return robot, nil
		},
	}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 9, accID: 18000000, slot: 0, slotKnown: true}
	vo.mu.Lock()
	vo.ensurePartySupervisorUnsafe()
	vo.mu.Unlock()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		vo.mu.Lock()
		connected := vo.partyRelayConn != nil
		vo.mu.Unlock()
		if connected && calls.Load() >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	vo.mu.Lock()
	connected := vo.partyRelayConn != nil
	vo.mu.Unlock()
	if !connected || calls.Load() < 2 {
		t.Fatalf("relay did not reconnect connected=%t calls=%d", connected, calls.Load())
	}

	peer := <-connectedPeer
	vo.mu.Lock()
	vo.State = StateStop
	vo.stopPartySupervisorUnsafe()
	vo.closePartyRelayUnsafe()
	vo.mu.Unlock()
	_ = peer.Close()
	_ = gameConn.Close()
}

func TestPartySupervisorRecoversFromStepPanic(t *testing.T) {
	vo := &RobotVo{UID: 17000001, State: StateRun, Conn: panicLocalAddrConn{}}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 9, accID: 18000000, slot: 0, slotKnown: true}
	vo.mu.Lock()
	if !vo.ensurePartySupervisorUnsafe() {
		vo.mu.Unlock()
		t.Fatal("party supervisor did not start")
	}
	vo.mu.Unlock()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		vo.mu.Lock()
		running := vo.partySupervisorRun
		vo.mu.Unlock()
		if !running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("party supervisor stayed marked running after panic")
}

func TestPartyRelayCombinesRepliesIntoOneWrite(t *testing.T) {
	robot, relay := net.Pipe()
	defer relay.Close()
	vo := &RobotVo{UID: 17000001, State: StateRun, partyRelayConn: robot}
	vo.partySelfPeer = partyIPPeer{uniqueID: 2, accID: 17000001, slot: 1, slotKnown: true}
	peer := partyIPPeer{uniqueID: 1, accID: 18000000, slot: 0, slotKnown: true}
	vo.partyPeers[0] = peer
	payload := append(buildPartyReliablePacket(7, peer.slot, 0, [][]byte{{1, 2, 3}}), buildPartyReliablePacket(8, peer.slot, 0, [][]byte{{4, 5, 6}})...)
	vo.handlePartyRelayPacket(robot, buildPartyRelayPacket(3, peer.accID, vo.UID, payload))
	packet := readPartyRelayPacket(t, relay)
	frames, ok := splitPartyTransportFrames(packet[12:])
	if !ok || len(frames) != 2 || !bytes.Equal(frames[0], buildPartyTQOSAck(vo.partySelfPeer.slot, 7)) || !bytes.Equal(frames[1], buildPartyTQOSAck(vo.partySelfPeer.slot, 8)) {
		t.Fatalf("combined relay replies=%x frames=%x ok=%t", packet, frames, ok)
	}
	vo.mu.Lock()
	vo.State = StateStop
	vo.closePartyRelayUnsafe()
	vo.mu.Unlock()
}
