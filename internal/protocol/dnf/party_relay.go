package dnf

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

const (
	partyRelayWriteTimeout   = 500 * time.Millisecond
	partyRelayWriteQueueSize = 64
	partyRelayBackoffInitial = 500 * time.Millisecond
	partyRelayBackoffMax     = 30 * time.Second
	partyRelayMaxPacketSize  = 4096
)

type partyRelayDialFunc func(network, address string, timeout time.Duration) (net.Conn, error)

type partyRelayWriter struct {
	conn    net.Conn
	packets chan []byte
	done    chan struct{}
}

func (r *RobotVo) ensurePartyRelayUnsafe() {
	if r.partyRelayConn != nil || r.partyRelayConnecting || r.State != StateRun || r.Conn == nil || !r.partyActiveUnsafe() || r.LoginIP == "" {
		return
	}
	host := r.LoginIP
	now := time.Now()
	if !r.partyRelayNextAt.IsZero() && now.Before(r.partyRelayNextAt) {
		return
	}
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
		if conn == nil && r.partyRelayConn == nil && r.State == StateRun && r.partyActiveUnsafe() {
			r.schedulePartyRelayRetryUnsafe(time.Now())
		}
		return false
	}
	r.partyRelayConn = conn
	r.partyRelayNextAt = time.Time{}
	r.partyRelayBackoff = 0
	for slot := byte(0); slot < 4; slot++ {
		r.partyRouteBlockedUntil[slot][2] = time.Time{}
		r.partyRouteFailures[slot][2] = 0
	}
	r.startPartyRelayWriterUnsafe(conn)
	fmt.Printf("[PARTY_RELAY_CONNECTED] uid=%d\n", r.UID)
	return true
}

func (r *RobotVo) closePartyRelayUnsafe() {
	r.partyRelayGeneration++
	r.partyRelayConnecting = false
	r.partyRelayNextAt = time.Time{}
	r.partyRelayBackoff = 0
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
	if r.State == StateRun && r.partyActiveUnsafe() {
		now := time.Now()
		for slot := byte(0); slot < 4; slot++ {
			peer := r.partyPeerForSlotUnsafe(slot)
			if partyPeerIdentityKnown(peer) && r.partyRouteInUseUnsafe(slot, 2) {
				r.markPartyRouteFailureUnsafe(peer, 2, now, "relay disconnected")
			}
		}
		r.schedulePartyRelayRetryUnsafe(now)
	}
	return r.State != StateStop
}

func (r *RobotVo) schedulePartyRelayRetryUnsafe(now time.Time) {
	if r.partyRelayBackoff <= 0 {
		r.partyRelayBackoff = partyRelayBackoffInitial
	} else {
		r.partyRelayBackoff *= 2
		if r.partyRelayBackoff > partyRelayBackoffMax {
			r.partyRelayBackoff = partyRelayBackoffMax
		}
	}
	r.partyRelayNextAt = now.Add(r.partyRelayBackoff)
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
				r.mu.Lock()
				stopped := r.State == StateStop || r.partyRelayConn != conn
				r.mu.Unlock()
				if stopped {
					_ = conn.Close()
					return
				}
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
			if size < 12 || size > partyRelayMaxPacketSize {
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
	for _, replyPayload := range groupPartyTransportFrames(replies, partyRelayMaxPacketSize-12) {
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
