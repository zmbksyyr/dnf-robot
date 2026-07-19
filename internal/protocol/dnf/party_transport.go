package dnf

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"
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

func (r *RobotVo) flushPartyRuntimeUnsafe(conn *net.UDPConn, now time.Time) {
	r.flushPartyTQOSRepliesUnsafe(conn, now)
	r.flushPartyDungeonFollowUnsafe(conn, now)
	r.flushPartyDungeonSkillUnsafe(conn, now)
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
