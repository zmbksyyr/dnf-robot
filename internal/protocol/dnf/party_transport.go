package dnf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"math/bits"
	"net"
	"strconv"
	"time"
)

const (
	partyRelayWriteTimeout   = 500 * time.Millisecond
	partyRelayWriteQueueSize = 64
)

const partyRobotProbeCooldown = 30 * time.Second

type partyRelayDialFunc func(network, address string, timeout time.Duration) (net.Conn, error)

type partyRelayWriter struct {
	conn    net.Conn
	packets chan []byte
	done    chan struct{}
}

func (r *RobotVo) sendPartyOptionUnsafe() bool {
	if r.State != StateRun || !r.partyOptionReady || r.partyOptionSent {
		return false
	}
	var body [80]byte
	binary.LittleEndian.PutUint32(body[0:4], gameEtcOptionSize)
	copy(body[4:], r.partyOptionData[:])
	pkt, err := buildSendPacket(200, uint16(r.PacketID), body[:], r.Cipher)
	r.PacketID++
	if err != nil {
		fmt.Printf("[PARTY_OPTION_BUILD_ERROR] uid=%d err=%v\n", r.UID, err)
		return false
	}
	if !r.sendRaw(pkt) {
		fmt.Printf("[PARTY_OPTION_SEND_ERROR] uid=%d\n", r.UID)
		return false
	}
	r.partyOptionSent = true
	return true
}

func (r *RobotVo) sendNATInfoUnsafe() bool {
	if r.State != StateRun || r.natInfoSent || r.Conn == nil {
		return false
	}
	addr, ok := r.Conn.LocalAddr().(*net.TCPAddr)
	if !ok {
		return false
	}
	if !r.startPartyUDPUnsafe(addr) {
		return false
	}
	udpAddr, ok := r.partyUDPConn.LocalAddr().(*net.UDPAddr)
	if !ok || udpAddr.Port <= 0 || udpAddr.Port > 0xffff {
		return false
	}
	body, ok := buildNATInfoPayload(udpAddr.IP, uint16(udpAddr.Port))
	if !ok {
		return false
	}
	pkt, err := buildSendPacket(2, uint16(r.PacketID), body, r.Cipher)
	r.PacketID++
	if err != nil {
		fmt.Printf("[NAT_BUILD_ERROR] uid=%d err=%v\n", r.UID, err)
		return false
	}
	if !r.sendRaw(pkt) {
		fmt.Printf("[NAT_SEND_ERROR] uid=%d\n", r.UID)
		return false
	}
	r.natInfoSent = true
	return true
}

func (r *RobotVo) ensurePartyRelayUnsafe() {
	if r.partyRelayConn != nil || r.partyRelayConnecting || r.State != StateRun || r.Conn == nil || !r.partyActiveUnsafe() || r.LoginIP == "" {
		return
	}
	host := r.LoginIP
	now := time.Now()
	if !r.partyRelayAt.IsZero() && now.Sub(r.partyRelayAt) < 5*time.Second {
		return
	}
	r.partyRelayAt = now
	relayAddr := net.JoinHostPort(host, strconv.Itoa(currentPartyRelayPort()))
	r.partyRelayConnecting = true
	generation := r.partyRelayGeneration
	uid := r.UID
	dial := r.partyRelayDial
	if dial == nil {
		dial = net.DialTimeout
	}
	go r.connectPartyRelay(generation, uid, relayAddr, dial)
}

func (r *RobotVo) connectPartyRelay(generation uint64, uid uint32, relayAddr string, dial partyRelayDialFunc) {
	conn, err := dial("tcp", relayAddr, 3*time.Second)
	if err != nil {
		r.finishPartyRelayConnect(generation, nil)
		fmt.Printf("[PARTY_RELAY_CONNECT_ERROR] uid=%d addr=%s err=%v\n", uid, relayAddr, err)
		return
	}
	auth := buildPartyRelayPacket(0, uid, 0, nil)
	if err := r.writePartyRelayConn(conn, auth); err != nil {
		_ = conn.Close()
		r.finishPartyRelayConnect(generation, nil)
		fmt.Printf("[PARTY_RELAY_AUTH_ERROR] uid=%d err=%v\n", uid, err)
		return
	}
	if !r.finishPartyRelayConnect(generation, conn) {
		_ = conn.Close()
		return
	}
	go r.partyRelayLoop(conn, uid)
}

func (r *RobotVo) finishPartyRelayConnect(generation uint64, conn net.Conn) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if generation != r.partyRelayGeneration {
		return false
	}
	r.partyRelayConnecting = false
	if conn == nil || r.partyRelayConn != nil || r.State != StateRun || !r.partyActiveUnsafe() {
		return false
	}
	r.partyRelayConn = conn
	r.startPartyRelayWriterUnsafe(conn)
	return true
}

func (r *RobotVo) closePartyRelayUnsafe() {
	r.partyRelayGeneration++
	r.partyRelayConnecting = false
	conn := r.partyRelayConn
	r.partyRelayConn = nil
	r.stopPartyRelayWriterUnsafe(conn)
	if conn != nil {
		_ = conn.Close()
	}
}

func (r *RobotVo) detachPartyRelayConn(conn net.Conn) bool {
	if conn == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.detachPartyRelayConnUnsafe(conn)
}

func (r *RobotVo) detachPartyRelayConnUnsafe(conn net.Conn) bool {
	if conn == nil || r.partyRelayConn != conn {
		return false
	}
	r.partyRelayConn = nil
	r.partyRelayGeneration++
	r.partyRelayConnecting = false
	r.stopPartyRelayWriterUnsafe(conn)
	return r.State != StateStop
}

func (r *RobotVo) writePartyRelayConn(conn net.Conn, packet []byte) error {
	if conn == nil {
		return fmt.Errorf("party relay is not connected")
	}
	if err := conn.SetWriteDeadline(time.Now().Add(partyRelayWriteTimeout)); err != nil {
		return err
	}
	for len(packet) > 0 {
		n, err := conn.Write(packet)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrUnexpectedEOF
		}
		packet = packet[n:]
	}
	return nil
}

func (r *RobotVo) startPartyRelayWriterUnsafe(conn net.Conn) *partyRelayWriter {
	if conn == nil {
		return nil
	}
	if writer := r.partyRelayWriter; writer != nil && writer.conn == conn {
		return writer
	}
	if r.partyRelayWriter != nil {
		close(r.partyRelayWriter.done)
	}
	writer := &partyRelayWriter{
		conn:    conn,
		packets: make(chan []byte, partyRelayWriteQueueSize),
		done:    make(chan struct{}),
	}
	r.partyRelayWriter = writer
	go r.partyRelayWriteLoop(writer, r.UID)
	return writer
}

func (r *RobotVo) stopPartyRelayWriterUnsafe(conn net.Conn) {
	writer := r.partyRelayWriter
	if writer == nil || (conn != nil && writer.conn != conn) {
		return
	}
	r.partyRelayWriter = nil
	close(writer.done)
}

func (r *RobotVo) partyRelayWriteLoop(writer *partyRelayWriter, uid uint32) {
	if writer == nil {
		return
	}
	for {
		select {
		case <-writer.done:
			return
		default:
		}
		select {
		case <-writer.done:
			return
		case packet := <-writer.packets:
			if err := r.writePartyRelayConn(writer.conn, packet); err != nil {
				unexpected := r.detachPartyRelayConn(writer.conn)
				_ = writer.conn.Close()
				if unexpected {
					fmt.Printf("[PARTY_RELAY_WRITE_ERROR] uid=%d err=%v\n", uid, err)
				}
				return
			}
		}
	}
}

func (r *RobotVo) enqueuePartyRelayPacketUnsafe(conn net.Conn, packet []byte) error {
	if conn == nil || r.partyRelayConn != conn {
		return fmt.Errorf("party relay is not connected")
	}
	writer := r.startPartyRelayWriterUnsafe(conn)
	if writer == nil {
		return fmt.Errorf("party relay writer is unavailable")
	}
	packet = append([]byte(nil), packet...)
	select {
	case writer.packets <- packet:
		return nil
	default:
		return fmt.Errorf("party relay write queue is full")
	}
}

func (r *RobotVo) enqueuePartyRelayPacket(conn net.Conn, packet []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.enqueuePartyRelayPacketUnsafe(conn, packet)
}

func (r *RobotVo) partyRelayLoop(conn net.Conn, uid uint32) {
	buf := make([]byte, 4096)
	pending := make([]byte, 0, 4096)
	nextHeartbeat := time.Now().Add(10 * time.Second)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, err := conn.Read(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				now := time.Now()
				if now.After(nextHeartbeat) {
					if err := r.enqueuePartyRelayPacket(conn, buildPartyRelayPacket(1, uid, uid, nil)); err != nil {
						unexpected := r.detachPartyRelayConn(conn)
						_ = conn.Close()
						if unexpected {
							fmt.Printf("[PARTY_RELAY_HEARTBEAT_ERROR] uid=%d err=%v\n", uid, err)
						}
						return
					}
					nextHeartbeat = now.Add(10 * time.Second)
				}
				r.mu.Lock()
				stopped := r.State == StateStop || r.partyRelayConn != conn
				r.mu.Unlock()
				if stopped {
					_ = conn.Close()
					return
				}
				continue
			}
			unexpected := r.detachPartyRelayConn(conn)
			_ = conn.Close()
			if unexpected {
				fmt.Printf("[PARTY_RELAY_READ_ERROR] uid=%d err=%v\n", uid, err)
			}
			return
		}
		if n <= 0 {
			continue
		}
		pending = append(pending, buf[:n]...)
		for len(pending) >= 12 {
			size := int(binary.LittleEndian.Uint16(pending[2:4]))
			if size < 12 || size > 4096 {
				unexpected := r.detachPartyRelayConn(conn)
				_ = conn.Close()
				if unexpected {
					fmt.Printf("[PARTY_RELAY_BAD_PACKET] uid=%d size=%d\n", uid, size)
				}
				return
			}
			if len(pending) < size {
				break
			}
			packet := append([]byte(nil), pending[:size]...)
			pending = pending[size:]
			r.handlePartyRelayPacket(conn, packet)
		}
	}
}

func (r *RobotVo) handlePartyRelayPacket(conn net.Conn, packet []byte) {
	if len(packet) < 12 {
		return
	}
	typ := binary.LittleEndian.Uint16(packet[0:2])
	src := binary.LittleEndian.Uint32(packet[4:8])
	dst := binary.LittleEndian.Uint32(packet[8:12])
	payload := packet[12:]
	if typ != 3 || src == 0 || src == r.UID || dst != r.UID || len(payload) == 0 {
		return
	}
	replies := r.buildPartyRelayReplies(payload, src)
	for _, replyPayload := range replies {
		reply := buildPartyRelayPacket(1, r.UID, src, replyPayload)
		if err := r.enqueuePartyRelayPacket(conn, reply); err != nil {
			unexpected := r.detachPartyRelayConn(conn)
			_ = conn.Close()
			if unexpected {
				fmt.Printf("[PARTY_RELAY_REPLY_ERROR] uid=%d dst=%d err=%v\n", r.UID, src, err)
			}
			return
		}
	}
}

func (r *RobotVo) startPartyUDPUnsafe(addr *net.TCPAddr) bool {
	if addr == nil {
		return false
	}
	if r.partyUDPConn != nil {
		if udpAddr, ok := r.partyUDPConn.LocalAddr().(*net.UDPAddr); ok && udpAddr.Port == addr.Port {
			r.ensurePartyUDPLoopUnsafe()
			return true
		}
		r.closePartyUDPUnsafe()
	}
	udpAddr := &net.UDPAddr{IP: addr.IP, Port: addr.Port}
	conn, err := net.ListenUDP("udp4", udpAddr)
	if err != nil {
		fallback := &net.UDPAddr{IP: addr.IP}
		conn, err = net.ListenUDP("udp4", fallback)
		if err != nil {
			fmt.Printf("[PARTY_UDP_LISTEN_ERROR] uid=%d ip=%s port=%d err=%v\n", r.UID, addr.IP.String(), addr.Port, err)
			return false
		}
		actual := conn.LocalAddr().(*net.UDPAddr)
		fmt.Printf("[PARTY_UDP_PORT_FALLBACK] uid=%d requested=%d actual=%d\n", r.UID, addr.Port, actual.Port)
	}
	r.partyUDPConn = conn
	r.partyUDPGeneration++
	r.partyUDPRunning = false
	r.ensurePartyUDPLoopUnsafe()
	return true
}

func (r *RobotVo) closePartyUDPUnsafe() {
	r.partyUDPGeneration++
	r.partyUDPRunning = false
	if r.partyUDPConn != nil {
		_ = r.partyUDPConn.Close()
		r.partyUDPConn = nil
	}
}

func (r *RobotVo) ensurePartyUDPLoopUnsafe() bool {
	if r.partyUDPConn == nil || r.partyUDPRunning || !r.partyActiveUnsafe() {
		return false
	}
	conn := r.partyUDPConn
	generation := r.partyUDPGeneration
	r.partyUDPRunning = true
	go r.partyUDPLoop(conn, r.UID, generation)
	return true
}

func (r *RobotVo) partyUDPLoop(conn *net.UDPConn, uid uint32, generation uint64) {
	buf := make([]byte, 4096)
	var nextProbe time.Time
	var nextFlush time.Time
	for {
		now := time.Now()
		r.mu.Lock()
		stopped := r.State == StateStop || r.partyUDPConn != conn || r.partyUDPGeneration != generation
		active := false
		if !stopped {
			active = r.partyActiveUnsafe()
			if active {
				if nextProbe.IsZero() || !now.Before(nextProbe) {
					r.startPartyRobotPeerNegotiationUnsafe()
					nextProbe = now.Add(time.Second)
				}
				if nextFlush.IsZero() || !now.Before(nextFlush) {
					r.flushPartyDungeonFollowUnsafe(conn, now)
					r.flushPartyDungeonSkillUnsafe(conn, now)
					nextFlush = now.Add(100 * time.Millisecond)
				}
			}
		}
		if stopped || !active {
			if r.partyUDPConn == conn && r.partyUDPGeneration == generation {
				r.partyUDPRunning = false
			}
			r.mu.Unlock()
			return
		}
		r.mu.Unlock()
		readWait := time.Until(nextFlush)
		if probeWait := time.Until(nextProbe); probeWait < readWait {
			readWait = probeWait
		}
		if readWait < time.Millisecond {
			readWait = time.Millisecond
		}
		_ = conn.SetReadDeadline(time.Now().Add(readWait))
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			r.mu.Lock()
			if r.partyUDPConn == conn && r.partyUDPGeneration == generation {
				r.partyUDPRunning = false
			}
			r.mu.Unlock()
			return
		}
		if n <= 0 || remote == nil {
			continue
		}
		payload := append([]byte(nil), buf[:n]...)
		if shouldReplyPartyUDP(conn, remote) {
			acks := r.buildPartyUDPAcks(payload, remote)
			for _, ack := range acks {
				writePartyUDPReply(conn, ack, remote, uid)
			}
		}
	}
}

func shouldReplyPartyUDP(conn *net.UDPConn, remote *net.UDPAddr) bool {
	if conn == nil || remote == nil || remote.IP == nil {
		return false
	}
	local, ok := conn.LocalAddr().(*net.UDPAddr)
	if ok && local.IP != nil && local.IP.Equal(remote.IP) && local.Port == remote.Port {
		return false
	}
	return true
}

func writePartyUDPReply(conn *net.UDPConn, payload []byte, remote *net.UDPAddr, uid uint32) {
	if conn == nil || remote == nil {
		return
	}
	if _, err := conn.WriteToUDP(payload, remote); err != nil {
		fmt.Printf("[PARTY_UDP_ACK_ERROR] uid=%d remote=%s err=%v\n", uid, remote.String(), err)
	}
}

func (r *RobotVo) buildPartyUDPAcks(payload []byte, remote *net.UDPAddr) [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	var senderSlot *byte
	if len(payload) >= 8 && (payload[0] == 0x01 || payload[0] == 0x02) {
		slot := payload[7]
		senderSlot = &slot
	}
	peer, ok := r.partyPeerForUDPUnsafe(remote, senderSlot)
	if !ok {
		r.tracePartyUDPUnsafe("DROP_PEER", remote, senderSlot, 0, 0)
		return nil
	}
	exactEndpoint := partyPeerEndpointMatches(peer.observedIP, peer.observedPort, remote) || partyPeerEndpointMatches(peer.outerIP, peer.port, remote)
	if !exactEndpoint {
		if senderSlot == nil || !r.partyTQOSPayloadAuthenticatesPeerUnsafe(payload, 1, peer) {
			r.tracePartyUDPUnsafe("DROP_ENDPOINT", remote, senderSlot, peer.accID, len(payload))
			return nil
		}
		peer = r.learnPartyPeerEndpointUnsafe(peer, remote)
	}
	r.tracePartyUDPUnsafe("RECV", remote, senderSlot, peer.accID, len(payload))
	if len(payload) == 8 && payload[0] == 0x00 {
		return nil
	}
	return r.buildPartyTQOSRepliesUnsafe(payload, 1, peer)
}

func (r *RobotVo) partyTQOSPayloadAuthenticatesPeerUnsafe(payload []byte, route byte, peer partyIPPeer) bool {
	if !peer.slotKnown || route < 1 || route > 2 {
		return false
	}
	frames, ok := splitPartyTransportFrames(payload)
	if !ok {
		return false
	}
	var preferred *partyTQOSCodec
	if peer.slot < 4 && r.partyTQOSCodecKnown[peer.slot][route] {
		codec := r.partyTQOSCodecs[peer.slot][route]
		preferred = &codec
	}
	for _, frame := range frames {
		if len(frame) < 9 || frame[0] == 0 || frame[7] != peer.slot {
			continue
		}
		request, ok := parsePartyTQOSPacketWithCodec(frame, route, preferred)
		if ok && request.senderSlot == peer.slot && request.route == route {
			return true
		}
	}
	return false
}

func (r *RobotVo) buildPartyRelayReplies(payload []byte, src uint32) [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	peer, ok := r.partyPeerForAccountUnsafe(src)
	if !ok {
		return nil
	}
	return r.buildPartyTQOSRepliesUnsafe(payload, 2, peer)
}

func (r *RobotVo) buildPartyTQOSRepliesUnsafe(payload []byte, route byte, peer partyIPPeer) [][]byte {
	if !r.partySelfPeer.slotKnown || !peer.slotKnown || peer.slot >= 4 || route >= 3 {
		return nil
	}
	frames, ok := splitPartyTransportFrames(payload)
	if !ok {
		return nil
	}
	replies := make([][]byte, 0, len(frames)+1)
	now := time.Now()
	for _, frame := range frames {
		if frame[0] == 0x00 {
			if route == 1 && r.partyTQOSReplyAcknowledgedUnsafe(frame, route, peer) {
				r.markPartyRobotPeerReadyUnsafe(peer, "ack")
			}
			continue
		}
		if frame[7] != peer.slot {
			return nil
		}
		r.rememberPartyPeerRouteUnsafe(peer.slot, route, now)
		if frame[0] == 0x01 {
			sequence := binary.LittleEndian.Uint32(frame[1:5])
			replies = append(replies, buildPartyTQOSAck(r.partySelfPeer.slot, sequence))
			if !r.partyTQOSReceived[peer.slot][route].accept(sequence) {
				continue
			}
		}
		if r.shouldFollowPartyPeerUnsafe(peer) {
			r.rememberPartyDungeonActivityUnsafe(frame, route, peer, now)
			r.tracePartyDungeonFrameUnsafe(frame, route, peer)
			r.queuePartyDungeonFollowUnsafe(frame, peer, now)
		}
		var preferred *partyTQOSCodec
		if peer.slot < 4 && route < 3 && r.partyTQOSCodecKnown[peer.slot][route] {
			codec := r.partyTQOSCodecs[peer.slot][route]
			preferred = &codec
		}
		request, ok := parsePartyTQOSPacketWithCodec(frame, route, preferred)
		if !ok {
			continue
		}
		if route == 1 && request.state == 2 {
			r.markPartyRobotPeerReadyUnsafe(peer, "state2")
		}
		if peer.slot < 4 && route < 3 {
			r.partyTQOSCodecs[peer.slot][route] = request.codec
			r.partyTQOSCodecKnown[peer.slot][route] = true
		}
		nextState, hasNextState := nextPartyTQOSState(request.state)
		if hasNextState {
			var reply []byte
			if nextState == 2 {
				reply, ok = r.partyTQOSReliableReplyUnsafe(peer.slot, route, request)
			} else {
				var sequence uint32
				sequence, ok = r.nextPartyTQOSSequenceUnsafe(peer.slot, route, false)
				if ok {
					reply = buildPartyTQOSPacket(sequence, r.partySelfPeer.slot, request.flags, nextState, route, request.codec)
				}
			}
			if !ok {
				continue
			}
			replies = append(replies, reply)
		}
	}
	return replies
}

func (r *RobotVo) partyTQOSReplyAcknowledgedUnsafe(frame []byte, route byte, peer partyIPPeer) bool {
	if len(frame) != 8 || frame[0] != 0 || frame[1] != peer.slot || peer.slot >= byte(len(r.partyTQOSReplies)) || route >= byte(len(r.partyTQOSReplies[0])) {
		return false
	}
	packet := r.partyTQOSReplies[peer.slot][route].packet
	if len(packet) < 5 {
		return false
	}
	return binary.LittleEndian.Uint32(frame[2:6]) == binary.LittleEndian.Uint32(packet[1:5])+1
}

func (r *RobotVo) markPartyRobotPeerReadyUnsafe(peer partyIPPeer, reason string) {
	if peer.slot >= 4 || !isPartyRobotAccount(peer.accID) || r.partyRobotPeerReady[peer.slot] {
		return
	}
	r.partyRobotPeerReady[peer.slot] = true
	fmt.Printf("[PARTY_ROBOT_TQOS_READY] uid=%d peer=%d slot=%d reason=%s\n", r.UID, peer.accID, peer.slot, reason)
}

func (r *RobotVo) partyTQOSReliableReplyUnsafe(peerSlot, route byte, request partyTQOSPacket) ([]byte, bool) {
	if peerSlot >= byte(len(r.partyTQOSReplies)) || route >= byte(len(r.partyTQOSReplies[0])) {
		return nil, false
	}
	pending := &r.partyTQOSReplies[peerSlot][route]
	if pending.packet != nil {
		return pending.packet, true
	}
	sequence, ok := r.nextPartyTQOSSequenceUnsafe(peerSlot, route, true)
	if !ok {
		return nil, false
	}
	packet := buildPartyTQOSPacket(sequence, r.partySelfPeer.slot, request.flags, 2, route, request.codec)
	pending.packet = packet
	return packet, true
}

func (r *RobotVo) tracePartyUDPUnsafe(reason string, remote *net.UDPAddr, senderSlot *byte, peer uint32, size int) {
	if !isPartyRobotAccount(peer) && reason != "DROP_PEER" {
		return
	}
	if reason == "DROP_PEER" {
		if !isPartyRobotAccount(r.partySelfPeer.accID) || remote == nil || remote.IP == nil {
			return
		}
		localIP := r.partySelfPeer.outerIP
		if localIP == nil {
			localIP = r.partySelfPeer.innerIP
		}
		if localIP == nil || !localIP.Equal(remote.IP) {
			return
		}
	}
	now := time.Now()
	if now.Before(r.partyUDPDiagAt) {
		return
	}
	r.partyUDPDiagAt = now.Add(1500 * time.Millisecond)
	slot := -1
	if senderSlot != nil {
		slot = int(*senderSlot)
	}
	remoteText := "<nil>"
	if remote != nil {
		remoteText = remote.String()
	}
	fmt.Printf("[PARTY_ROBOT_UDP_%s] uid=%d peer=%d sender_slot=%d size=%d remote=%s\n", reason, r.UID, peer, slot, size, remoteText)
}

func (r *RobotVo) nextPartyTQOSSequenceUnsafe(peerSlot, route byte, reliable bool) (uint32, bool) {
	if peerSlot >= byte(len(r.partyTQOSSeq)) || route >= byte(len(r.partyTQOSSeq[0])) {
		return 0, false
	}
	if reliable {
		sequence := r.partyTQOSReliableSeq[peerSlot][route]
		r.partyTQOSReliableSeq[peerSlot][route]++
		return sequence, true
	}
	sequence := r.partyTQOSSeq[peerSlot][route]
	r.partyTQOSSeq[peerSlot][route]++
	return sequence, true
}

func (r *RobotVo) rememberPartyPeerRouteUnsafe(peerSlot, route byte, now time.Time) {
	if peerSlot >= byte(len(r.partyPeerRoute)) || (route != 1 && route != 2) {
		return
	}
	r.partyPeerRoute[peerSlot] = route
	r.partyPeerRouteAt[peerSlot] = now
}

func (r *RobotVo) partyRouteForPeerUnsafe(peerSlot byte) byte {
	peer := r.partyPeerForSlotUnsafe(peerSlot)
	if peerSlot < byte(len(r.partyPeerRoute)) && !r.partyPeerRouteAt[peerSlot].IsZero() {
		route := r.partyPeerRoute[peerSlot]
		if r.partyRouteAvailableUnsafe(peer, route) {
			return route
		}
	}
	if r.partyRouteAvailableUnsafe(peer, 1) {
		return 1
	}
	if r.partyRouteAvailableUnsafe(peer, 2) {
		return 2
	}
	return 1
}

func (r *RobotVo) partyRouteAvailableUnsafe(peer partyIPPeer, route byte) bool {
	switch route {
	case 1:
		_, ok := partyPeerUDPAddr(peer)
		return ok && r.partyUDPConn != nil
	case 2:
		return peer.accID != 0 && r.partyRelayConn != nil
	default:
		return false
	}
}

func (r *RobotVo) startPartyRobotPeerNegotiationUnsafe() {
	if r.partyUDPConn == nil || !r.partySelfPeer.slotKnown || !isPartyRobotAccount(r.partySelfPeer.accID) {
		return
	}
	for _, peer := range r.partyPeers {
		if !peer.slotKnown || peer.slot >= 4 || r.partyRobotPeerReady[peer.slot] || !isPartyRobotAccount(peer.accID) {
			continue
		}
		if r.partySelfPeer.accID == peer.accID {
			continue
		}
		if _, ok := partyPeerUDPAddr(peer); !ok {
			continue
		}
		now := time.Now()
		if r.partyRobotProbeCount[peer.slot] >= 4 {
			if now.Sub(r.partyRobotProbeAt[peer.slot]) < partyRobotProbeCooldown {
				continue
			}
			r.partyRobotProbeCount[peer.slot] = 0
		}
		if !r.partyRobotProbeAt[peer.slot].IsZero() && now.Sub(r.partyRobotProbeAt[peer.slot]) < 750*time.Millisecond {
			continue
		}
		attempt := int(r.partyRobotProbeCount[peer.slot]) + 1
		if r.sendPartyRobotPeerProbeUnsafe(peer, attempt) {
			r.partyRobotProbeAt[peer.slot] = now
			r.partyRobotProbeCount[peer.slot]++
		}
	}
}

func (r *RobotVo) sendPartyRobotPeerProbeUnsafe(peer partyIPPeer, attempt int) bool {
	if r.partyUDPConn == nil || !peer.slotKnown || peer.slot >= 4 {
		return false
	}
	remote, ok := partyPeerUDPAddr(peer)
	if !ok {
		return false
	}
	sequence, ok := r.nextPartyTQOSSequenceUnsafe(peer.slot, 1, false)
	if !ok {
		return false
	}
	payload := buildPartyTQOSPacket(sequence, r.partySelfPeer.slot, 0, 3, 1, partyTQOSCodec{key: 0x7e})
	if _, err := r.partyUDPConn.WriteToUDP(payload, remote); err != nil {
		fmt.Printf("[PARTY_ROBOT_PROBE_ERROR] uid=%d peer=%d attempt=%d remote=%s err=%v\n", r.UID, peer.accID, attempt, remote.String(), err)
		return false
	}
	fmt.Printf("[PARTY_ROBOT_PROBE] uid=%d peer=%d slot=%d attempt=%d sequence=%d remote=%s\n", r.UID, peer.accID, peer.slot, attempt, sequence, remote.String())
	return true
}

func (r *RobotVo) sendPartyTransportUnsafe(conn *net.UDPConn, peer partyIPPeer, route byte, payload []byte) (string, error) {
	switch route {
	case 1:
		remote, ok := partyPeerUDPAddr(peer)
		if !ok {
			return "", fmt.Errorf("party peer UDP endpoint is unavailable")
		}
		if conn == nil {
			return remote.String(), fmt.Errorf("party UDP socket is unavailable")
		}
		_, err := conn.WriteToUDP(payload, remote)
		return remote.String(), err
	case 2:
		if peer.accID == 0 {
			return "relay", fmt.Errorf("party peer account is unavailable")
		}
		relay := r.partyRelayConn
		if relay == nil {
			return "relay", fmt.Errorf("party relay is unavailable")
		}
		err := r.enqueuePartyRelayPacketUnsafe(relay, buildPartyRelayPacket(1, r.UID, peer.accID, payload))
		if err != nil && r.partyRelayConn == relay {
			r.detachPartyRelayConnUnsafe(relay)
			_ = relay.Close()
		}
		return "relay", err
	default:
		return "", fmt.Errorf("unsupported party route %d", route)
	}
}

func (r *RobotVo) partyPeerForUDPUnsafe(remote *net.UDPAddr, senderSlot *byte) (partyIPPeer, bool) {
	if remote == nil || remote.IP == nil || remote.Port <= 0 || remote.Port > 0xffff {
		return partyIPPeer{}, false
	}
	for i := range r.partyPeers {
		peer := r.partyPeers[i]
		if peer.uniqueID == 0 {
			continue
		}
		if senderSlot != nil && (!peer.slotKnown || peer.slot != *senderSlot) {
			continue
		}
		if partyPeerEndpointMatches(peer.observedIP, peer.observedPort, remote) || partyPeerEndpointMatches(peer.outerIP, peer.port, remote) {
			return peer, true
		}
	}
	if senderSlot == nil {
		return partyIPPeer{}, false
	}
	for i := range r.partyPeers {
		peer := r.partyPeers[i]
		if peer.uniqueID == 0 || !peer.slotKnown || peer.slot != *senderSlot || !partyPeerKnownIP(peer, remote.IP) {
			continue
		}
		return peer, true
	}
	return partyIPPeer{}, false
}

func (r *RobotVo) learnPartyPeerEndpointUnsafe(peer partyIPPeer, remote *net.UDPAddr) partyIPPeer {
	if remote == nil || remote.IP == nil || remote.Port <= 0 || remote.Port > 0xffff || !peer.slotKnown || !partyPeerKnownIP(peer, remote.IP) {
		return peer
	}
	for i := range r.partyPeers {
		if r.partyPeers[i].uniqueID != peer.uniqueID || !r.partyPeers[i].slotKnown || r.partyPeers[i].slot != peer.slot {
			continue
		}
		r.partyPeers[i].observedIP = append(net.IP(nil), remote.IP...)
		r.partyPeers[i].observedPort = uint16(remote.Port)
		return r.partyPeers[i]
	}
	return peer
}

func partyPeerEndpointMatches(ip net.IP, port uint16, remote *net.UDPAddr) bool {
	return ip != nil && port != 0 && remote != nil && remote.IP != nil && ip.Equal(remote.IP) && port == uint16(remote.Port)
}

func partyPeerKnownIP(peer partyIPPeer, ip net.IP) bool {
	if ip == nil {
		return false
	}
	return (peer.innerIP != nil && peer.innerIP.Equal(ip)) ||
		(peer.outerIP != nil && peer.outerIP.Equal(ip)) ||
		(peer.observedIP != nil && peer.observedIP.Equal(ip))
}

func partyPeerUDPAddr(peer partyIPPeer) (*net.UDPAddr, bool) {
	if peer.observedIP != nil && peer.observedPort != 0 {
		return &net.UDPAddr{IP: peer.observedIP, Port: int(peer.observedPort)}, true
	}
	if peer.outerIP == nil || peer.port == 0 {
		return nil, false
	}
	return &net.UDPAddr{IP: peer.outerIP, Port: int(peer.port)}, true
}

func (r *RobotVo) resetPartyTQOSTransportUnsafe() {
	r.partyTQOSSeq = [4][3]uint32{}
	r.partyTQOSReliableSeq = [4][3]uint32{}
	r.partyTQOSReplies = [4][3]partyTQOSReliableReply{}
	r.partyTQOSReceived = [4][3]partyTQOSReceiveWindow{}
	r.partyTQOSCodecs = [4][3]partyTQOSCodec{}
	r.partyTQOSCodecKnown = [4][3]bool{}
	r.partyRobotProbeAt = [4]time.Time{}
	r.partyRobotProbeCount = [4]uint8{}
	r.partyRobotPeerReady = [4]bool{}
	r.partyPeerRoute = [4]byte{}
	r.partyPeerRouteAt = [4]time.Time{}
	r.partyDungeonFollow = nil
	r.partyDungeonLastAt = time.Time{}
	r.partyDungeonFlags = 0
	r.partySkillNextAt = time.Time{}
	r.partySkillRecoverAt = time.Time{}
}

func (r *RobotVo) resetPartyTQOSPeerUnsafe(slot byte) {
	if slot >= byte(len(r.partyTQOSSeq)) {
		return
	}
	r.partyTQOSSeq[slot] = [3]uint32{}
	r.partyTQOSReliableSeq[slot] = [3]uint32{}
	r.partyTQOSReplies[slot] = [3]partyTQOSReliableReply{}
	r.partyTQOSReceived[slot] = [3]partyTQOSReceiveWindow{}
	r.partyTQOSCodecs[slot] = [3]partyTQOSCodec{}
	r.partyTQOSCodecKnown[slot] = [3]bool{}
	r.partyRobotProbeAt[slot] = time.Time{}
	r.partyRobotProbeCount[slot] = 0
	r.partyRobotPeerReady[slot] = false
	r.partyPeerRoute[slot] = 0
	r.partyPeerRouteAt[slot] = time.Time{}
	if len(r.partyDungeonFollow) > 0 {
		kept := r.partyDungeonFollow[:0]
		for _, pending := range r.partyDungeonFollow {
			if pending.peerSlot != slot {
				kept = append(kept, pending)
			}
		}
		r.partyDungeonFollow = kept
	}
}

func partyPeerUniqueIDForSlot(peers [4]partyIPPeer, slot byte) uint16 {
	for _, peer := range peers {
		if peer.slotKnown && peer.slot == slot {
			return peer.uniqueID
		}
	}
	return 0
}

func buildNATInfoPayload(ip net.IP, port uint16) ([]byte, bool) {
	ipv4 := ip.To4()
	if ipv4 == nil {
		return nil, false
	}
	const (
		marker        = "robot"
		defaultUDPMTU = 1472
	)
	body := make([]byte, 24)
	body[0] = 1
	copy(body[1:5], ipv4)
	copy(body[5:9], ipv4)
	binary.BigEndian.PutUint16(body[9:11], port)
	binary.LittleEndian.PutUint32(body[11:15], defaultUDPMTU)
	binary.LittleEndian.PutUint32(body[15:19], uint32(len(marker)))
	copy(body[19:], marker)
	return body, true
}

const partyTQOSBodySize = 10

var partyTQOSCRCTable = crc32.MakeTable(0x4db89129)

type partyTQOSCodec struct {
	key    byte
	rotate uint8
}

type partyTQOSPacket struct {
	typ        byte
	sequence   uint32
	senderSlot byte
	flags      byte
	state      byte
	route      byte
	codec      partyTQOSCodec
}

type partyTQOSReliableReply struct {
	packet []byte
}

type partyTQOSReceiveWindow struct {
	latest      uint32
	seen        uint64
	initialized bool
}

func (w *partyTQOSReceiveWindow) accept(sequence uint32) bool {
	if !w.initialized {
		w.latest = sequence
		w.seen = 1
		w.initialized = true
		return true
	}
	delta := int32(sequence - w.latest)
	if delta > 0 {
		if delta >= 64 {
			w.seen = 1
		} else {
			w.seen = (w.seen << uint(delta)) | 1
		}
		w.latest = sequence
		return true
	}
	behind := uint32(w.latest - sequence)
	if behind >= 64 {
		return false
	}
	mask := uint64(1) << behind
	if w.seen&mask != 0 {
		return false
	}
	w.seen |= mask
	return true
}

func splitPartyTransportFrames(payload []byte) ([][]byte, bool) {
	frames := make([][]byte, 0, 2)
	for len(payload) > 0 {
		frameSize := 0
		switch payload[0] {
		case 0x00:
			frameSize = 8
		case 0x01, 0x02:
			if len(payload) < 9 {
				return nil, false
			}
			frameSize = 9 + int(binary.LittleEndian.Uint16(payload[5:7]))
		default:
			return nil, false
		}
		if frameSize <= 0 || frameSize > len(payload) {
			return nil, false
		}
		frames = append(frames, payload[:frameSize])
		payload = payload[frameSize:]
	}
	return frames, len(frames) > 0
}

func buildPartyUnreliablePacket(sequence uint32, senderSlot, flags byte, body []byte) []byte {
	payload := make([]byte, 9+len(body))
	payload[0] = 0x02
	binary.LittleEndian.PutUint32(payload[1:5], sequence)
	binary.LittleEndian.PutUint16(payload[5:7], uint16(len(body)))
	payload[7] = senderSlot
	payload[8] = flags
	copy(payload[9:], body)
	return payload
}

func buildPartyReliablePacket(sequence uint32, senderSlot, flags byte, records [][]byte) []byte {
	bodySize := 0
	for _, record := range records {
		bodySize += 2 + len(record)
	}
	payload := make([]byte, 9, 9+bodySize)
	payload[0] = 0x01
	binary.LittleEndian.PutUint32(payload[1:5], sequence)
	binary.LittleEndian.PutUint16(payload[5:7], uint16(bodySize))
	payload[7] = senderSlot
	payload[8] = flags
	for _, record := range records {
		sizeOffset := len(payload)
		payload = append(payload, 0, 0)
		binary.LittleEndian.PutUint16(payload[sizeOffset:], uint16(len(record)))
		payload = append(payload, record...)
	}
	return payload
}

func parsePartyTQOSPacket(payload []byte, expectedRoute byte) (partyTQOSPacket, bool) {
	return parsePartyTQOSPacketWithCodec(payload, expectedRoute, nil)
}

func parsePartyTQOSPacketWithCodec(payload []byte, expectedRoute byte, preferred *partyTQOSCodec) (partyTQOSPacket, bool) {
	if len(payload) < 9 || (payload[0] != 0x01 && payload[0] != 0x02) {
		return partyTQOSPacket{}, false
	}
	bodySize := int(binary.LittleEndian.Uint16(payload[5:7]))
	if len(payload) != 9+bodySize {
		return partyTQOSPacket{}, false
	}
	body := payload[9:]
	if payload[0] == 0x01 && len(body) != partyTQOSBodySize {
		if len(body) < 2 {
			return partyTQOSPacket{}, false
		}
		innerSize := int(binary.LittleEndian.Uint16(body[0:2]))
		if innerSize != partyTQOSBodySize || len(body) < 2+innerSize {
			return partyTQOSPacket{}, false
		}
		body = body[2 : 2+innerSize]
	}
	if len(body) != partyTQOSBodySize {
		return partyTQOSPacket{}, false
	}
	if body[0] != 0 || body[1] != 0 || body[2] != 0 {
		return partyTQOSPacket{}, false
	}

	senderSlot := payload[7]
	state, codec, ok := decodePartyTQOSBody(body, senderSlot, expectedRoute, preferred)
	if !ok {
		return partyTQOSPacket{}, false
	}
	return partyTQOSPacket{
		typ:        payload[0],
		sequence:   binary.LittleEndian.Uint32(payload[1:5]),
		senderSlot: senderSlot,
		flags:      payload[8],
		state:      state,
		route:      expectedRoute,
		codec:      codec,
	}, true
}

func decodePartyTQOSBody(body []byte, senderSlot, expectedRoute byte, preferred *partyTQOSCodec) (byte, partyTQOSCodec, bool) {
	if len(body) != partyTQOSBodySize {
		return 0, partyTQOSCodec{}, false
	}
	if preferred != nil {
		if state, ok := decodePartyTQOSBodyWithCodec(body, senderSlot, expectedRoute, *preferred); ok {
			return state, *preferred, true
		}
	}
	for rotate := 0; rotate < 8; rotate++ {
		key := bits.RotateLeft8(body[7], -rotate) ^ senderSlot
		codec := partyTQOSCodec{key: key, rotate: uint8(rotate)}
		if state, ok := decodePartyTQOSBodyWithCodec(body, senderSlot, expectedRoute, codec); ok {
			return state, codec, true
		}
	}
	return 0, partyTQOSCodec{}, false
}

func decodePartyTQOSBodyWithCodec(body []byte, senderSlot, expectedRoute byte, codec partyTQOSCodec) (byte, bool) {
	decodedSlot := bits.RotateLeft8(body[7], -int(codec.rotate)) ^ codec.key
	state := bits.RotateLeft8(body[8], -int(codec.rotate)) ^ codec.key
	route := bits.RotateLeft8(body[9], -int(codec.rotate)) ^ codec.key
	if decodedSlot != senderSlot || state > 3 || route != expectedRoute {
		return 0, false
	}
	checksum := partyTQOSChecksum(senderSlot, state, route)
	return state, bytes.Equal(body[3:7], checksum[:])
}

func buildPartyTQOSPacket(sequence uint32, senderSlot, flags, state, route byte, codec partyTQOSCodec) []byte {
	bodySize := partyTQOSBodySize
	bodyOffset := 9
	typ := byte(0x02)
	if state == 2 {
		typ = 0x01
		bodySize += 2
		bodyOffset += 2
	}
	payload := make([]byte, 9+bodySize)
	payload[0] = typ
	binary.LittleEndian.PutUint32(payload[1:5], sequence)
	binary.LittleEndian.PutUint16(payload[5:7], uint16(bodySize))
	payload[7] = senderSlot
	payload[8] = flags
	if typ == 0x01 {
		binary.LittleEndian.PutUint16(payload[9:11], partyTQOSBodySize)
	}
	body := payload[bodyOffset:]
	checksum := partyTQOSChecksum(senderSlot, state, route)
	copy(body[3:7], checksum[:])
	plain := [3]byte{senderSlot, state, route}
	for i, value := range plain {
		body[7+i] = bits.RotateLeft8(value^codec.key, int(codec.rotate))
	}
	return payload
}

func buildPartyTQOSAck(senderSlot byte, sequence uint32) []byte {
	payload := make([]byte, 8)
	payload[1] = senderSlot
	binary.LittleEndian.PutUint32(payload[2:6], sequence+1)
	return payload
}

func nextPartyTQOSState(state byte) (byte, bool) {
	switch state {
	case 3:
		return 0, true
	case 0:
		return 1, true
	case 1:
		return 2, true
	default:
		return 0, false
	}
}

func partyTQOSChecksum(senderSlot, state, route byte) [4]byte {
	return partyPayloadChecksum([]byte{senderSlot, state, route})
}

func partyPayloadChecksum(payload []byte) [4]byte {
	value := crc32.Checksum(payload, partyTQOSCRCTable)
	var checksum [4]byte
	binary.LittleEndian.PutUint32(checksum[:], value)
	checksum[0] ^= checksum[1] ^ checksum[2] ^ checksum[3] ^ 0x18
	return checksum
}

func buildPartyRelayPacket(typ uint16, src, dst uint32, payload []byte) []byte {
	size := 12 + len(payload)
	body := make([]byte, size)
	binary.LittleEndian.PutUint16(body[0:2], typ)
	binary.LittleEndian.PutUint16(body[2:4], uint16(size))
	binary.LittleEndian.PutUint32(body[4:8], src)
	binary.LittleEndian.PutUint32(body[8:12], dst)
	copy(body[12:], payload)
	return body
}
