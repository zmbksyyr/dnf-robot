package network

import (
	"bytes"
	"fmt"
	"net"
	"sync"
	"time"
)

const defaultListenAddr = "0.0.0.0:8188"
const maxSendLength = 204800
const defaultMaxClients = 256
const defaultReadTimeout = 90 * time.Second
const defaultWriteTimeout = 15 * time.Second
const defaultFirstPacketTimeout = 5 * time.Second
const defaultMaxPendingPerIP = 512

type TCPMsgHeader struct {
	MsgID   uint32
	MsgType uint32
	MsgSize uint32
}

type ClientInfo struct {
	Conn net.Conn
	Addr string
	UID  string
}

type TCPServer struct {
	listener     net.Listener
	addr         string
	clients      map[string]*ClientInfo
	connCount    int
	pendingByIP  map[string]int
	clientsMu    sync.RWMutex
	onMessage    func(clientID string, data []byte)
	onClose      func(clientID string)
	running      bool
	stopCh       chan struct{}
	wg           sync.WaitGroup
	maxClients   int
	readTimeout  time.Duration
	writeTimeout time.Duration
	firstTimeout time.Duration
	maxPendingIP int
}

func NewTCPServer(addr string) *TCPServer {
	if addr == "" {
		addr = defaultListenAddr
	}
	return &TCPServer{
		addr:         addr,
		clients:      make(map[string]*ClientInfo),
		pendingByIP:  make(map[string]int),
		stopCh:       make(chan struct{}),
		maxClients:   defaultMaxClients,
		readTimeout:  defaultReadTimeout,
		writeTimeout: defaultWriteTimeout,
		firstTimeout: defaultFirstPacketTimeout,
		maxPendingIP: defaultMaxPendingPerIP,
	}
}

func (s *TCPServer) SetLimits(maxClients int, readTimeout, writeTimeout time.Duration) {
	if maxClients > 0 {
		s.maxClients = maxClients
	}
	if readTimeout > 0 {
		s.readTimeout = readTimeout
	}
	if writeTimeout > 0 {
		s.writeTimeout = writeTimeout
	}
}

func (s *TCPServer) SetHandshakeLimits(firstPacketTimeout time.Duration, maxPendingPerIP int) {
	if firstPacketTimeout > 0 {
		s.firstTimeout = firstPacketTimeout
	}
	if maxPendingPerIP > 0 {
		s.maxPendingIP = maxPendingPerIP
	}
}

func (s *TCPServer) OnMessage(callback func(clientID string, data []byte)) {
	s.onMessage = callback
}

func (s *TCPServer) OnClose(callback func(clientID string)) {
	s.onClose = callback
}

func (s *TCPServer) Start() error {
	var err error
	s.listener, err = net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("TCPServer listen %s: %w", s.addr, err)
	}
	s.running = true

	go s.acceptLoop()
	return nil
}

func (s *TCPServer) acceptLoop() {
	for s.running {
		conn, err := s.listener.Accept()
		if err != nil {
			if !s.running {
				return
			}
			continue
		}
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			_ = tcpConn.SetNoDelay(true)
			_ = tcpConn.SetKeepAlive(true)
			_ = tcpConn.SetKeepAlivePeriod(30 * time.Second)
		}

		clientID := conn.RemoteAddr().String()
		clientIP := remoteIP(conn.RemoteAddr())
		s.clientsMu.Lock()
		if s.maxPendingIP > 0 && s.pendingByIP[clientIP] >= s.maxPendingIP {
			s.clientsMu.Unlock()
			_ = conn.Close()
			continue
		}
		client := &ClientInfo{
			Conn: conn,
			Addr: clientID,
		}
		s.pendingByIP[clientIP]++
		s.clientsMu.Unlock()

		s.wg.Add(1)
		go s.handleClient(clientID, clientIP, client)
	}
}

func (s *TCPServer) handleClient(clientID, clientIP string, client *ClientInfo) {
	defer s.wg.Done()
	firstPacketSeen := false
	registered := false
	defer func() {
		s.clientsMu.Lock()
		if registered {
			delete(s.clients, clientID)
			if client.UID != "" {
				delete(s.clients, client.UID)
			}
			s.connCount--
		}
		if !firstPacketSeen && s.pendingByIP[clientIP] > 0 {
			s.pendingByIP[clientIP]--
			if s.pendingByIP[clientIP] == 0 {
				delete(s.pendingByIP, clientIP)
			}
		}
		s.clientsMu.Unlock()
		if s.onClose != nil {
			s.onClose(clientID)
		}
		client.Conn.Close()
	}()

	buf := make([]byte, 4096)
	recvBuf := make([]byte, 0, maxSendLength)

	for {
		timeout := s.readTimeout
		if !firstPacketSeen && s.firstTimeout > 0 {
			timeout = s.firstTimeout
		}
		if timeout > 0 {
			_ = client.Conn.SetReadDeadline(time.Now().Add(timeout))
		}
		n, err := client.Conn.Read(buf)
		if err != nil {
			return
		}
		if n > 0 {
			recvBuf = append(recvBuf, buf[:n]...)
			for {
				data, remaining, ok := tryExtractXMLPacket(recvBuf)
				if !ok {
					break
				}
				recvBuf = remaining
				if !firstPacketSeen {
					if !s.registerFirstPacket(clientID, clientIP, client) {
						return
					}
					registered = true
					firstPacketSeen = true
				}
				if s.onMessage != nil {
					s.onMessage(clientID, data)
				}
			}
			if len(recvBuf) > maxSendLength {
				return
			}
		}
	}
}

func tryExtractXMLPacket(buf []byte) (packet []byte, remaining []byte, ok bool) {
	if len(buf) < len("<tw></tw>") {
		return nil, buf, false
	}
	start := bytes.IndexByte(buf, '<')
	if start < 0 {
		keep := len(buf)
		if keep > 32 {
			keep = 32
		}
		return nil, buf[len(buf)-keep:], false
	}
	if start > 0 {
		buf = buf[start:]
	}
	end := bytes.Index(buf, []byte("</tw>"))
	if end < 0 {
		return nil, buf, false
	}
	packetLen := end + len("</tw>")
	packet = buf[:packetLen]
	remaining = buf[packetLen:]
	if len(remaining) > 0 {
		if next := bytes.IndexByte(remaining, '<'); next >= 0 {
			remaining = remaining[next:]
		} else {
			remaining = nil
		}
	}
	return packet, remaining, true
}

func (s *TCPServer) registerFirstPacket(clientID, clientIP string, client *ClientInfo) bool {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	if s.maxClients > 0 && s.connCount >= s.maxClients {
		return false
	}
	if s.pendingByIP[clientIP] > 0 {
		s.pendingByIP[clientIP]--
		if s.pendingByIP[clientIP] == 0 {
			delete(s.pendingByIP, clientIP)
		}
	}
	s.clients[clientID] = client
	s.connCount++
	return true
}

func remoteIP(addr net.Addr) string {
	if tcp, ok := addr.(*net.TCPAddr); ok {
		return tcp.IP.String()
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err == nil {
		return host
	}
	return addr.String()
}

func (s *TCPServer) SendTo(clientID string, data []byte) error {
	s.clientsMu.RLock()
	client, ok := s.clients[clientID]
	s.clientsMu.RUnlock()
	if !ok {
		return fmt.Errorf("client %s not found", clientID)
	}
	if s.writeTimeout > 0 {
		_ = client.Conn.SetWriteDeadline(time.Now().Add(s.writeTimeout))
	}
	_, err := client.Conn.Write(data)
	return err
}

func (s *TCPServer) Broadcast(data []byte) {
	s.clientsMu.RLock()
	clients := make([]*ClientInfo, 0, len(s.clients))
	for _, c := range s.clients {
		clients = append(clients, c)
	}
	s.clientsMu.RUnlock()

	for _, c := range clients {
		if s.writeTimeout > 0 {
			_ = c.Conn.SetWriteDeadline(time.Now().Add(s.writeTimeout))
		}
		c.Conn.Write(data)
	}
}

func (s *TCPServer) BindClient(clientID string, uid string) {
	s.clientsMu.Lock()
	if client, ok := s.clients[clientID]; ok {
		client.UID = uid
		s.clients[uid] = client
	}
	s.clientsMu.Unlock()
}

func (s *TCPServer) GetClient(clientID string) *ClientInfo {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	return s.clients[clientID]
}

func (s *TCPServer) Close() error {
	s.running = false
	close(s.stopCh)
	if s.listener != nil {
		s.listener.Close()
	}
	s.clientsMu.Lock()
	for _, c := range s.clients {
		c.Conn.Close()
	}
	s.clients = make(map[string]*ClientInfo)
	s.clientsMu.Unlock()
	s.wg.Wait()
	return nil
}

func (s *TCPServer) IsRunning() bool {
	return s.running
}
