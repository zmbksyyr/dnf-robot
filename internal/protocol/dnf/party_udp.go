package dnf

import (
	"errors"
	"fmt"
	"net"
	"time"
)

const (
	partyUDPReadErrorLogGap  = 5 * time.Second
	partyUDPReadBackoffMin   = 10 * time.Millisecond
	partyUDPReadBackoffMax   = time.Second
	partyUDPReadRecycleAfter = 5 * time.Second
)

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
	localIP := conn.LocalAddr().(*net.UDPAddr).IP
	r.partyUDPMTU = partyUDPMTUForIP(localIP)
	r.partyUDPNetwork = partyIPv4NetworkForIP(localIP)
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
	r.partyUDPMTU = 0
	r.partyUDPNetwork = nil
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
	mtu := partyUDPMTUForConn(conn)
	var readErrorLogAt time.Time
	var readErrorBackoff time.Duration
	var readErrorSince time.Time
	for {
		r.mu.Lock()
		stopped := r.State == StateStop || r.partyUDPConn != conn || r.partyUDPGeneration != generation
		active := !stopped && r.partyActiveUnsafe()
		if stopped || !active {
			if r.partyUDPConn == conn && r.partyUDPGeneration == generation {
				r.partyUDPRunning = false
			}
			r.mu.Unlock()
			return
		}
		r.mu.Unlock()
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			r.mu.Lock()
			active := r.State != StateStop && r.partyUDPConn == conn && r.partyUDPGeneration == generation && r.partyActiveUnsafe()
			if !active || partyUDPReadErrorTerminal(err) {
				if r.partyUDPConn == conn && r.partyUDPGeneration == generation {
					r.partyUDPRunning = false
				}
				r.mu.Unlock()
				return
			}
			now := time.Now()
			if readErrorSince.IsZero() {
				readErrorSince = now
			}
			if readErrorLogAt.IsZero() || !now.Before(readErrorLogAt) {
				fmt.Printf("[PARTY_UDP_READ_ERROR] uid=%d err=%v\n", uid, err)
				readErrorLogAt = now.Add(partyUDPReadErrorLogGap)
			}
			if now.Sub(readErrorSince) >= partyUDPReadRecycleAfter {
				if r.partyUDPConn == conn && r.partyUDPGeneration == generation {
					r.partyUDPRunning = false
				}
				r.mu.Unlock()
				fmt.Printf("[PARTY_UDP_RECYCLE] uid=%d err=%v\n", uid, err)
				return
			}
			r.mu.Unlock()
			readErrorBackoff = nextPartyUDPReadBackoff(readErrorBackoff)
			time.Sleep(readErrorBackoff)
			continue
		}
		readErrorBackoff = 0
		readErrorSince = time.Time{}
		if n <= 0 || remote == nil {
			continue
		}
		payload := append([]byte(nil), buf[:n]...)
		if shouldReplyPartyUDP(conn, remote) {
			replies, groupErr := groupPartyTransportFrames(r.buildPartyUDPAcks(payload, remote), mtu)
			if groupErr != nil {
				fmt.Printf("[PARTY_UDP_ACK_ERROR] uid=%d remote=%s err=%v\n", uid, remote.String(), groupErr)
				continue
			}
			for _, reply := range replies {
				writePartyUDPReply(conn, reply, remote, uid, mtu)
			}
		}
	}
}

func partyUDPReadErrorTerminal(err error) bool {
	return errors.Is(err, net.ErrClosed)
}

func nextPartyUDPReadBackoff(current time.Duration) time.Duration {
	if current < partyUDPReadBackoffMin {
		return partyUDPReadBackoffMin
	}
	if current >= partyUDPReadBackoffMax/2 {
		return partyUDPReadBackoffMax
	}
	return current * 2
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

func writePartyUDPReply(conn *net.UDPConn, payload []byte, remote *net.UDPAddr, uid uint32, mtu int) {
	if conn == nil || remote == nil {
		return
	}
	if err := writePartyUDPDatagramWithMTU(conn, payload, remote, mtu); err != nil {
		recordPartyTransportFrames(uid, 0, "TX", "UDP", 1, "FAIL", "dst="+remote.String(), payload)
		fmt.Printf("[PARTY_UDP_ACK_ERROR] uid=%d remote=%s err=%v\n", uid, remote.String(), err)
		return
	}
	recordPartyTransportFrames(uid, 0, "TX", "UDP", 1, "OK", "dst="+remote.String(), payload)
}

func (r *RobotVo) partyPeerForUDPUnsafe(remote *net.UDPAddr, senderSlot *byte) (partyIPPeer, bool) {
	if remote == nil || remote.IP == nil || remote.Port <= 0 || remote.Port > 0xffff {
		return partyIPPeer{}, false
	}
	for i := range r.partyPeers {
		peer := r.partyPeers[i]
		if peer.uniqueID == 0 && peer.accID == 0 {
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
		if (peer.uniqueID == 0 && peer.accID == 0) || !peer.slotKnown || peer.slot != *senderSlot || !partyPeerKnownIP(peer, remote.IP) {
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
		if !partyPeerSameIdentity(r.partyPeers[i], peer) || !r.partyPeers[i].slotKnown || r.partyPeers[i].slot != peer.slot {
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
	remote, _, ok := partyPeerUDPAddrForLocal(peer, nil, false)
	return remote, ok
}

func partyPeerUDPAddrForLocal(peer partyIPPeer, localIP net.IP, alternate bool) (*net.UDPAddr, string, bool) {
	return partyPeerUDPAddrForNetwork(peer, partyIPv4NetworkForIP(localIP), alternate)
}

func partyPeerUDPAddrForNetwork(peer partyIPPeer, localNetwork *net.IPNet, alternate bool) (*net.UDPAddr, string, bool) {
	if peer.observedIP != nil && peer.observedPort != 0 {
		return &net.UDPAddr{IP: peer.observedIP, Port: int(peer.observedPort)}, "OBSERVED", true
	}
	if peer.port == 0 {
		return nil, "", false
	}
	innerFirst := peer.innerIP != nil && localNetwork != nil && localNetwork.Contains(peer.innerIP)
	primary, secondary := peer.outerIP, peer.innerIP
	primarySource, secondarySource := "OUTER", "INNER"
	if innerFirst {
		primary, secondary = peer.innerIP, peer.outerIP
		primarySource, secondarySource = "INNER", "OUTER"
	}
	if alternate && secondary != nil && (primary == nil || !secondary.Equal(primary)) {
		primary, primarySource = secondary, secondarySource
	}
	if primary == nil {
		return nil, "", false
	}
	return &net.UDPAddr{IP: primary, Port: int(peer.port)}, primarySource, true
}

func partyIPv4NetworkForIP(localIP net.IP) *net.IPNet {
	localIP = localIP.To4()
	if localIP == nil {
		return nil
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, iface := range interfaces {
		addrs, addrErr := iface.Addrs()
		if addrErr != nil {
			continue
		}
		for _, addr := range addrs {
			ip, network, parseErr := net.ParseCIDR(addr.String())
			if parseErr == nil && ip.Equal(localIP) {
				return network
			}
		}
	}
	return nil
}
