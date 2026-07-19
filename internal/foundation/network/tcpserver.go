package network

import (
	"bytes"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"robot/internal/foundation/lockhub"
)

const (
	defaultListenAddr         = "0.0.0.0:8188"
	maxReceiveBufferSize      = 204800
	initialReceiveBufferSize  = 4096
	defaultMaxClients         = 256
	defaultReadTimeout        = 90 * time.Second
	defaultWriteTimeout       = 15 * time.Second
	defaultFirstPacketTimeout = 5 * time.Second
	defaultMaxPendingPerIP    = 512
)

type tcpClient struct {
	conn net.Conn
}

type TCPServer struct {
	listener     net.Listener
	addr         string
	clients      map[string]*tcpClient
	connCount    int
	pendingByIP  map[string]int
	clientsMu    lockhub.RWLocker
	onMessage    func(clientID string, data []byte)
	running      atomic.Bool
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
		clients:      make(map[string]*tcpClient),
		pendingByIP:  make(map[string]int),
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

func (s *TCPServer) OnMessage(callback func(clientID string, data []byte)) {
	s.onMessage = callback
}

func (s *TCPServer) Start() error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("TCPServer listen %s: %w", s.addr, err)
	}
	s.listener = listener
	s.running.Store(true)
	s.wg.Add(1)
	go s.acceptLoop()
	return nil
}

func (s *TCPServer) acceptLoop() {
	defer s.wg.Done()
	for s.running.Load() {
		conn, err := s.listener.Accept()
		if err != nil {
			if !s.running.Load() {
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
		if !s.running.Load() {
			s.clientsMu.Unlock()
			_ = conn.Close()
			return
		}
		if s.maxPendingIP > 0 && s.pendingByIP[clientIP] >= s.maxPendingIP {
			s.clientsMu.Unlock()
			_ = conn.Close()
			continue
		}
		client := &tcpClient{conn: conn}
		s.clients[clientID] = client
		s.pendingByIP[clientIP]++
		s.clientsMu.Unlock()

		s.wg.Add(1)
		go s.handleClient(clientID, clientIP, client)
	}
}

func (s *TCPServer) handleClient(clientID, clientIP string, client *tcpClient) {
	defer s.wg.Done()
	firstPacketSeen := false
	registered := false
	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, clientID)
		if registered {
			s.connCount--
		}
		if !firstPacketSeen && s.pendingByIP[clientIP] > 0 {
			s.pendingByIP[clientIP]--
			if s.pendingByIP[clientIP] == 0 {
				delete(s.pendingByIP, clientIP)
			}
		}
		s.clientsMu.Unlock()
		_ = client.conn.Close()
	}()

	buf := make([]byte, 4096)
	recvBuf := make([]byte, 0, initialReceiveBufferSize)

	for {
		timeout := s.readTimeout
		if !firstPacketSeen && s.firstTimeout > 0 {
			timeout = s.firstTimeout
		}
		if timeout > 0 {
			_ = client.conn.SetReadDeadline(time.Now().Add(timeout))
		}
		n, err := client.conn.Read(buf)
		if err != nil {
			return
		}
		if n == 0 {
			continue
		}

		recvBuf = append(recvBuf, buf[:n]...)
		for {
			data, remaining, ok := tryExtractXMLPacket(recvBuf)
			if !ok {
				break
			}
			recvBuf = remaining
			if !firstPacketSeen {
				if !s.registerFirstPacket(clientIP) {
					return
				}
				registered = true
				firstPacketSeen = true
			}
			if s.onMessage != nil {
				s.onMessage(clientID, data)
			}
		}
		if len(recvBuf) > maxReceiveBufferSize {
			return
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

func (s *TCPServer) registerFirstPacket(clientIP string) bool {
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
		_ = client.conn.SetWriteDeadline(time.Now().Add(s.writeTimeout))
	}
	_, err := client.conn.Write(data)
	return err
}

func (s *TCPServer) Close() error {
	if !s.running.Swap(false) {
		return nil
	}
	if s.listener != nil {
		_ = s.listener.Close()
	}
	s.clientsMu.Lock()
	for _, client := range s.clients {
		_ = client.conn.Close()
	}
	s.clients = make(map[string]*tcpClient)
	s.clientsMu.Unlock()
	s.wg.Wait()
	return nil
}
