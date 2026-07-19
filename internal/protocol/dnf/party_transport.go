package dnf

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

const (
	partyDefaultUDPMTU   = 1472
	partyUDPWriteTimeout = 500 * time.Millisecond
)

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
	return r.sendNATInfoUpdateUnsafe(false)
}

func (r *RobotVo) sendNATInfoUpdateUnsafe(force bool) bool {
	if r.State != StateRun || (!force && r.natInfoSent) || r.Conn == nil {
		return false
	}
	addr, ok := r.Conn.LocalAddr().(*net.TCPAddr)
	if !ok {
		return false
	}
	if r.partyUDPConn == nil {
		if !r.startPartyUDPUnsafe(addr) {
			return false
		}
	} else {
		r.ensurePartyUDPLoopUnsafe()
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

func (r *RobotVo) flushPartyRuntimeUnsafe(conn *net.UDPConn, now time.Time) {
	r.flushPartyTQOSRepliesUnsafe(conn, now)
	r.flushPartyReliableUnsafe(conn, now)
	r.flushPartyDungeonFollowUnsafe(conn, now)
	r.flushPartyDungeonSkillUnsafe(conn, now)
}

func (r *RobotVo) sendPartyTransportUnsafe(conn *net.UDPConn, peer partyIPPeer, route byte, payload []byte) (string, error) {
	switch route {
	case 1:
		remote, ok := partyPeerUDPAddr(peer)
		if !ok {
			err := fmt.Errorf("party peer UDP endpoint is unavailable")
			r.markPartyRouteFailureUnsafe(peer, route, time.Now(), err.Error())
			return "", err
		}
		if conn == nil {
			err := fmt.Errorf("party UDP socket is unavailable")
			r.markPartyRouteFailureUnsafe(peer, route, time.Now(), err.Error())
			return remote.String(), err
		}
		err := writePartyUDPDatagram(conn, payload, remote)
		if err != nil {
			r.markPartyRouteFailureUnsafe(peer, route, time.Now(), err.Error())
		}
		return remote.String(), err
	case 2:
		if peer.accID == 0 {
			err := fmt.Errorf("party peer account is unavailable")
			r.markPartyRouteFailureUnsafe(peer, route, time.Now(), err.Error())
			return "relay", err
		}
		relay := r.partyRelayConn
		if relay == nil {
			err := fmt.Errorf("party relay is unavailable")
			r.markPartyRouteFailureUnsafe(peer, route, time.Now(), err.Error())
			return "relay", err
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

func writePartyUDPDatagram(conn *net.UDPConn, payload []byte, remote *net.UDPAddr) error {
	if conn == nil || remote == nil {
		return fmt.Errorf("party UDP destination is unavailable")
	}
	if err := conn.SetWriteDeadline(time.Now().Add(partyUDPWriteTimeout)); err != nil {
		return err
	}
	_, err := conn.WriteToUDP(payload, remote)
	return err
}

func buildNATInfoPayload(ip net.IP, port uint16) ([]byte, bool) {
	ipv4 := ip.To4()
	if ipv4 == nil {
		return nil, false
	}
	const (
		marker = "robot"
	)
	body := make([]byte, 24)
	body[0] = 1
	copy(body[1:5], ipv4)
	copy(body[5:9], ipv4)
	binary.BigEndian.PutUint16(body[9:11], port)
	binary.LittleEndian.PutUint32(body[11:15], partyDefaultUDPMTU)
	binary.LittleEndian.PutUint32(body[15:19], uint32(len(marker)))
	copy(body[19:], marker)
	return body, true
}

func groupPartyTransportFrames(frames [][]byte, maxSize int) [][]byte {
	groups := make([][]byte, 0, len(frames))
	for _, frame := range frames {
		if len(frame) == 0 {
			continue
		}
		last := len(groups) - 1
		if last < 0 || (maxSize > 0 && len(groups[last])+len(frame) > maxSize) {
			groups = append(groups, append([]byte(nil), frame...))
			continue
		}
		groups[last] = append(groups[last], frame...)
	}
	return groups
}
