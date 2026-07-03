package network

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"robot/internal/foundation/lockhub"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ---- netutil.go ----
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

func HTTPGet(rawURL string) (string, error) {
	resp, err := httpClient.Get(rawURL)
	if err != nil {
		return "", fmt.Errorf("HTTPGet %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("HTTPGet read body: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTPGet status %d: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

func HTTPPost(rawURL string, bodyStr string) (string, error) {
	resp, err := httpClient.Post(rawURL, "application/x-www-form-urlencoded", strings.NewReader(bodyStr))
	if err != nil {
		return "", fmt.Errorf("HTTPPost %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("HTTPPost read body: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTPPost status %d: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

func HTTPGetV5(rawURL string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return HTTPGet(rawURL)
	}

	req, err := http.NewRequest("GET", parsedURL.String(), nil)
	if err != nil {
		return HTTPGet(rawURL)
	}

	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Connection", "keep-alive")

	resp, err := httpClient.Do(req)
	if err != nil {
		return HTTPGet(rawURL)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("HTTPGetV5 read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTPGetV5 status %d: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

func HTTPPostV5(rawURL string, bodyStr string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return HTTPPost(rawURL, bodyStr)
	}

	data := url.Values{}
	data.Set("data", bodyStr)
	encoded := data.Encode()

	req, err := http.NewRequest("POST", parsedURL.String(), strings.NewReader(encoded))
	if err != nil {
		return HTTPPost(rawURL, bodyStr)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := httpClient.Do(req)
	if err != nil {
		return HTTPPost(rawURL, bodyStr)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("HTTPPostV5 read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTPPostV5 status %d: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

func ParseURL(u string) (host string, file string, port int, err error) {
	raw := strings.TrimSpace(u)
	if raw == "" {
		return "", "", 0, fmt.Errorf("empty url")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", 0, err
	}
	host = parsed.Hostname()
	if host == "" {
		return "", "", 0, fmt.Errorf("missing host")
	}
	switch p := parsed.Port(); {
	case p != "":
		port, err = strconv.Atoi(p)
		if err != nil || port <= 0 || port > 65535 {
			return "", "", 0, fmt.Errorf("invalid port %q", p)
		}
	case parsed.Scheme == "https":
		port = 443
	default:
		port = 80
	}
	file = strings.TrimPrefix(parsed.EscapedPath(), "/")
	if parsed.RawQuery != "" {
		file += "?" + parsed.RawQuery
	}
	return host, file, port, nil
}

// ---- proxy.go ----
const SystemID uint32 = 0xFFFFFFFF

type TCPMsgType uint32

const (
	SERVER_BEAT       TCPMsgType = 0
	CLIENT_BEAT       TCPMsgType = 1
	LIBUV_CONNECT     TCPMsgType = 2
	LIBUV_START_RECV  TCPMsgType = 3
	LIBUV_RECV        TCPMsgType = 4
	LIBUV_SEND_INIT1  TCPMsgType = 5
	LIBUV_SEND_INIT2  TCPMsgType = 6
	LIBUV_SEND_INIT3  TCPMsgType = 7
	LIBUV_SEND_INIT4  TCPMsgType = 8
	LIBUV_CLOSE_INIT1 TCPMsgType = 9
	LIBUV_CLOSE_INIT2 TCPMsgType = 10
	LIBUV_CLOSE_INIT3 TCPMsgType = 11
	LIBUV_SEND        TCPMsgType = 12
	LIBUV_CLOSE       TCPMsgType = 13
)

type ConnectReq struct {
	Port  uint16
	Delay uint32
}

type GeneralRes struct {
	Status int32
}

const msgHeaderSize = 12

type ProxyCallback func(clientID int32, status int32, buf []byte)

type ProxyCallbacks struct {
	Connect    ProxyCallback
	StartRecv  ProxyCallback
	Recv       ProxyCallback
	SendInit1  ProxyCallback
	SendInit2  ProxyCallback
	SendInit3  ProxyCallback
	SendInit4  ProxyCallback
	CloseInit1 ProxyCallback
	CloseInit2 ProxyCallback
	CloseInit3 ProxyCallback
	Close      ProxyCallback
	Send       ProxyCallback
}

type ProxyAppEvent int

const (
	AppWSAInitError ProxyAppEvent = iota
	AppSocketInitError
	AppBindError
	AppListenError
	AppWaitingAccept
	AppAcceptError
	AppAcceptOK
	AppLoseConnect
	AppDeadline
	AppSendDataError
	AppRecvDataError
)

type Proxy struct {
	listenPort uint16
	callbacks  *ProxyCallbacks

	listener     net.Listener
	serverConn   net.Conn
	serverConnMu lockhub.Locker

	beatCount   uint32
	beatCountMu lockhub.Locker

	recvBuf     []byte
	recvSize    uint32
	recvMaxSize uint32

	stopCh    chan struct{}
	closeOnce sync.Once
	running   atomic.Bool

	eventCallback func(event ProxyAppEvent, data string)

	connectField struct {
		ip    string
		port  uint16
		delay uint32
	}
}

func NewProxy(port uint16, cbs *ProxyCallbacks, eventCb func(event ProxyAppEvent, data string)) *Proxy {
	if cbs == nil {
		cbs = &ProxyCallbacks{}
	}
	return &Proxy{
		listenPort:    port,
		callbacks:     cbs,
		eventCallback: eventCb,
		recvMaxSize:   4096,
		recvBuf:       make([]byte, 4096),
		stopCh:        make(chan struct{}),
	}
}

func (p *Proxy) fireEvent(event ProxyAppEvent, data string) {
	if p.eventCallback != nil {
		p.eventCallback(event, data)
	}
}

func (p *Proxy) Init() error {
	var err error
	p.listener, err = net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", p.listenPort))
	if err != nil {
		p.fireEvent(AppSocketInitError, "")
		return fmt.Errorf("proxy listen: %w", err)
	}

	p.running.Store(true)

	go p.acceptWorkThread()

	return nil
}

func (p *Proxy) acceptWorkThread() {
	addr := fmt.Sprintf("0.0.0.0:%d", p.listenPort)
	_ = addr

	for p.running.Load() {
		p.fireEvent(AppWaitingAccept, "")

		conn, err := p.listener.Accept()
		if err != nil {
			if !p.running.Load() {
				return
			}
			p.fireEvent(AppAcceptError, "")
			continue
		}

		remoteAddr := conn.RemoteAddr().(*net.TCPAddr)
		p.fireEvent(AppAcceptOK, remoteAddr.IP.String())

		p.serverConnMu.Lock()
		if p.serverConn != nil {
			_ = p.serverConn.Close()
		}
		p.serverConn = conn
		p.serverConnMu.Unlock()

		go p.clientWorkThread()
		go p.clientBeatThread()
	}
}

func (p *Proxy) clientWorkThread() {
	buf := make([]byte, 1024)
	var active net.Conn
	defer func() {
		p.clearServerConn(active)
	}()
	for p.running.Load() {
		p.serverConnMu.Lock()
		conn := p.serverConn
		p.serverConnMu.Unlock()

		if conn == nil {
			return
		}
		active = conn

		n, err := conn.Read(buf)
		if err != nil {
			if err != io.EOF {
				p.fireEvent(AppLoseConnect, "")
			} else {
				p.fireEvent(AppLoseConnect, "")
			}
			return
		}
		if n > 0 {
			p.combineMessage(buf[:n])
		}
	}
}

func (p *Proxy) clientBeatThread() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for p.running.Load() {
		select {
		case <-ticker.C:
			p.beatCountMu.Lock()
			if p.beatCount >= 3 {
				p.beatCountMu.Unlock()
				p.fireEvent(AppLoseConnect, "")
				return
			}
			p.beatCount++
			p.beatCountMu.Unlock()

			header := make([]byte, msgHeaderSize)
			binary.LittleEndian.PutUint32(header[0:4], SystemID)
			binary.LittleEndian.PutUint32(header[4:8], uint32(CLIENT_BEAT))
			binary.LittleEndian.PutUint32(header[8:12], msgHeaderSize)
			if p.currentServerConn() == nil {
				return
			}
			p.serverSocketSend(header)

		case <-p.stopCh:
			return
		}
	}
}

func (p *Proxy) currentServerConn() net.Conn {
	p.serverConnMu.Lock()
	defer p.serverConnMu.Unlock()
	return p.serverConn
}

func (p *Proxy) clearServerConn(conn net.Conn) {
	if conn == nil {
		return
	}
	p.serverConnMu.Lock()
	if p.serverConn == conn {
		_ = p.serverConn.Close()
		p.serverConn = nil
	}
	p.serverConnMu.Unlock()
}

func (p *Proxy) serverSocketSend(data []byte) {
	p.serverConnMu.Lock()
	conn := p.serverConn
	p.serverConnMu.Unlock()

	if conn == nil {
		return
	}

	totalLen := len(data)
	chunkSize := 1024
	times := totalLen / chunkSize
	if totalLen%chunkSize != 0 {
		times++
	}
	remain := totalLen % chunkSize
	if remain == 0 && totalLen > 0 {
		remain = chunkSize
	}

	for i := 0; i < times-1; i++ {
		if _, err := conn.Write(data[i*chunkSize : (i+1)*chunkSize]); err != nil {
			p.fireEvent(AppSendDataError, err.Error())
			return
		}
	}
	if times > 0 {
		if _, err := conn.Write(data[(times-1)*chunkSize:]); err != nil {
			p.fireEvent(AppSendDataError, err.Error())
			return
		}
	}
}

func (p *Proxy) serverSocketRecv(data []byte) {
	if len(data) < msgHeaderSize {
		return
	}

	msgID := binary.LittleEndian.Uint32(data[0:4])
	msgType := TCPMsgType(binary.LittleEndian.Uint32(data[4:8]))
	msgSize := binary.LittleEndian.Uint32(data[8:12])
	if msgSize < msgHeaderSize || msgSize > uint32(len(data)) {
		p.fireEvent(AppRecvDataError, fmt.Sprintf("invalid message size=%d len=%d", msgSize, len(data)))
		return
	}

	bodyStart := msgHeaderSize

	switch msgType {
	case SERVER_BEAT:
		resp := make([]byte, msgHeaderSize)
		binary.LittleEndian.PutUint32(resp[0:4], SystemID)
		binary.LittleEndian.PutUint32(resp[4:8], uint32(SERVER_BEAT))
		binary.LittleEndian.PutUint32(resp[8:12], msgHeaderSize)
		p.serverSocketSend(resp)

	case CLIENT_BEAT:
		p.beatCountMu.Lock()
		p.beatCount = 0
		p.beatCountMu.Unlock()

	case LIBUV_CONNECT:
		if p.callbacks.Connect != nil {
			var res GeneralRes
			if len(data) >= bodyStart+4 {
				res.Status = int32(binary.LittleEndian.Uint32(data[bodyStart : bodyStart+4]))
			}
			p.callbacks.Connect(int32(msgID), res.Status, nil)
		}

	case LIBUV_START_RECV:
		if p.callbacks.StartRecv != nil {
			var res GeneralRes
			if len(data) >= bodyStart+4 {
				res.Status = int32(binary.LittleEndian.Uint32(data[bodyStart : bodyStart+4]))
			}
			p.callbacks.StartRecv(int32(msgID), res.Status, nil)
		}

	case LIBUV_RECV:
		if p.callbacks.Recv != nil {
			var res GeneralRes
			if len(data) >= bodyStart+4 {
				res.Status = int32(binary.LittleEndian.Uint32(data[bodyStart : bodyStart+4]))
			}
			if msgSize <= uint32(bodyStart+4) {
				p.fireEvent(AppRecvDataError, fmt.Sprintf("invalid recv body size=%d", msgSize))
				return
			}
			recvBody := data[bodyStart+4 : msgSize]
			p.callbacks.Recv(int32(msgID), res.Status, recvBody)
		}

	case LIBUV_SEND_INIT1:
		if p.callbacks.SendInit1 != nil {
			var res GeneralRes
			if len(data) >= bodyStart+4 {
				res.Status = int32(binary.LittleEndian.Uint32(data[bodyStart : bodyStart+4]))
			}
			p.callbacks.SendInit1(int32(msgID), res.Status, nil)
		}

	case LIBUV_SEND_INIT2:
		if p.callbacks.SendInit2 != nil {
			var res GeneralRes
			if len(data) >= bodyStart+4 {
				res.Status = int32(binary.LittleEndian.Uint32(data[bodyStart : bodyStart+4]))
			}
			p.callbacks.SendInit2(int32(msgID), res.Status, nil)
		}

	case LIBUV_SEND_INIT3:
		if p.callbacks.SendInit3 != nil {
			var res GeneralRes
			if len(data) >= bodyStart+4 {
				res.Status = int32(binary.LittleEndian.Uint32(data[bodyStart : bodyStart+4]))
			}
			p.callbacks.SendInit3(int32(msgID), res.Status, nil)
		}

	case LIBUV_SEND_INIT4:
		if p.callbacks.SendInit4 != nil {
			var res GeneralRes
			if len(data) >= bodyStart+4 {
				res.Status = int32(binary.LittleEndian.Uint32(data[bodyStart : bodyStart+4]))
			}
			p.callbacks.SendInit4(int32(msgID), res.Status, nil)
		}

	case LIBUV_CLOSE_INIT1:
		if p.callbacks.CloseInit1 != nil {
			var res GeneralRes
			if len(data) >= bodyStart+4 {
				res.Status = int32(binary.LittleEndian.Uint32(data[bodyStart : bodyStart+4]))
			}
			p.callbacks.CloseInit1(int32(msgID), res.Status, nil)
		}

	case LIBUV_CLOSE_INIT2:
		if p.callbacks.CloseInit2 != nil {
			var res GeneralRes
			if len(data) >= bodyStart+4 {
				res.Status = int32(binary.LittleEndian.Uint32(data[bodyStart : bodyStart+4]))
			}
			p.callbacks.CloseInit2(int32(msgID), res.Status, nil)
		}

	case LIBUV_CLOSE_INIT3:
		if p.callbacks.CloseInit3 != nil {
			var res GeneralRes
			if len(data) >= bodyStart+4 {
				res.Status = int32(binary.LittleEndian.Uint32(data[bodyStart : bodyStart+4]))
			}
			p.callbacks.CloseInit3(int32(msgID), res.Status, nil)
		}

	case LIBUV_SEND:
		if p.callbacks.Send != nil {
			var res GeneralRes
			if len(data) >= bodyStart+4 {
				res.Status = int32(binary.LittleEndian.Uint32(data[bodyStart : bodyStart+4]))
			}
			p.callbacks.Send(int32(msgID), res.Status, nil)
		}

	case LIBUV_CLOSE:
		if p.callbacks.Close != nil {
			var res GeneralRes
			if len(data) >= bodyStart+4 {
				res.Status = int32(binary.LittleEndian.Uint32(data[bodyStart : bodyStart+4]))
			}
			p.callbacks.Close(int32(msgID), res.Status, nil)
		}
	}
}

func (p *Proxy) combineMessage(inBuf []byte) {
	inSize := uint32(len(inBuf))

	if p.recvMaxSize-p.recvSize < inSize {
		for p.recvMaxSize-p.recvSize < inSize {
			p.recvMaxSize *= 2
		}
		newBuf := make([]byte, p.recvMaxSize)
		copy(newBuf, p.recvBuf[:p.recvSize])
		p.recvBuf = newBuf
	}

	copy(p.recvBuf[p.recvSize:], inBuf)
	p.recvSize += inSize

	for p.recvSize >= msgHeaderSize {
		msgSize := binary.LittleEndian.Uint32(p.recvBuf[8:12])
		if msgSize < msgHeaderSize {
			p.fireEvent(AppRecvDataError, fmt.Sprintf("invalid message size=%d", msgSize))
			p.recvSize = 0
			return
		}

		if p.recvSize >= msgSize {
			msg := make([]byte, msgSize)
			copy(msg, p.recvBuf[:msgSize])
			p.serverSocketRecv(msg)

			if p.recvSize > msgSize {
				copy(p.recvBuf, p.recvBuf[msgSize:p.recvSize])
			}
			p.recvSize -= msgSize
		} else {
			break
		}
	}

	if float64(p.recvSize) < 0.25*float64(p.recvMaxSize) {
		for float64(p.recvSize) < 0.25*float64(p.recvMaxSize) && p.recvMaxSize > 4096 {
			p.recvMaxSize /= 2
		}
		newBuf := make([]byte, p.recvMaxSize)
		copy(newBuf, p.recvBuf[:p.recvSize])
		p.recvBuf = newBuf
	}
}

type ProxyCommandType int

const (
	CmdConnect ProxyCommandType = iota
	CmdStartRecv
	CmdSend
	CmdClose
	CmdSendInit1
	CmdSendInit2
	CmdSendInit3
	CmdSendInit4
	CmdCloseInit1
	CmdCloseInit2
	CmdCloseInit3
)

type ProxyCommand struct {
	ClientID int32
	DataBuf  []byte
	CmdType  ProxyCommandType
}

func (p *Proxy) SendConnectCommand(clientID int32, port uint16, delay uint32) {
	headerSize := uint32(msgHeaderSize)
	bodySize := uint32(6)
	totalSize := headerSize + bodySize

	buf := make([]byte, totalSize)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(clientID))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(LIBUV_CONNECT))
	binary.LittleEndian.PutUint32(buf[8:12], totalSize)
	binary.LittleEndian.PutUint16(buf[12:14], port)
	binary.LittleEndian.PutUint32(buf[14:18], delay)

	p.serverSocketSend(buf)
}

func (p *Proxy) SendStartRecvCommand(clientID int32) {
	buf := make([]byte, msgHeaderSize)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(clientID))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(LIBUV_START_RECV))
	binary.LittleEndian.PutUint32(buf[8:12], msgHeaderSize)

	p.serverSocketSend(buf)
}

func (p *Proxy) SendSendCommand(clientID int32, sendBuf []byte) {
	sendSize := uint32(len(sendBuf))
	totalSize := msgHeaderSize + sendSize

	buf := make([]byte, totalSize)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(clientID))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(LIBUV_SEND))
	binary.LittleEndian.PutUint32(buf[8:12], totalSize)
	copy(buf[msgHeaderSize:], sendBuf)

	p.serverSocketSend(buf)
}

func (p *Proxy) SendSendInitCommand(clientID int32, initNum int, sendBuf []byte) {
	sendSize := uint32(len(sendBuf))
	totalSize := msgHeaderSize + sendSize

	msgTypes := []TCPMsgType{LIBUV_SEND_INIT1, LIBUV_SEND_INIT2, LIBUV_SEND_INIT3, LIBUV_SEND_INIT4}
	if initNum < 1 || initNum > 4 {
		return
	}

	buf := make([]byte, totalSize)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(clientID))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(msgTypes[initNum-1]))
	binary.LittleEndian.PutUint32(buf[8:12], totalSize)
	copy(buf[msgHeaderSize:], sendBuf)

	p.serverSocketSend(buf)
}

func (p *Proxy) SendCloseCommand(clientID int32) {
	buf := make([]byte, msgHeaderSize)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(clientID))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(LIBUV_CLOSE))
	binary.LittleEndian.PutUint32(buf[8:12], msgHeaderSize)

	p.serverSocketSend(buf)
}

func (p *Proxy) SendCloseInitCommand(clientID int32, initNum int) {
	msgTypes := []TCPMsgType{LIBUV_CLOSE_INIT1, LIBUV_CLOSE_INIT2, LIBUV_CLOSE_INIT3}
	if initNum < 1 || initNum > 3 {
		return
	}

	buf := make([]byte, msgHeaderSize)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(clientID))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(msgTypes[initNum-1]))
	binary.LittleEndian.PutUint32(buf[8:12], msgHeaderSize)

	p.serverSocketSend(buf)
}

func (p *Proxy) SendCommand(cmd ProxyCommand) {
	switch cmd.CmdType {
	case CmdConnect:
		port := uint16(0)
		delay := uint32(0)
		if len(cmd.DataBuf) >= 6 {
			port = binary.LittleEndian.Uint16(cmd.DataBuf[0:2])
			delay = binary.LittleEndian.Uint32(cmd.DataBuf[2:6])
		}
		p.SendConnectCommand(cmd.ClientID, port, delay)

	case CmdStartRecv:
		p.SendStartRecvCommand(cmd.ClientID)

	case CmdClose:
		p.SendCloseCommand(cmd.ClientID)

	case CmdCloseInit1:
		p.SendCloseInitCommand(cmd.ClientID, 1)

	case CmdCloseInit2:
		p.SendCloseInitCommand(cmd.ClientID, 2)

	case CmdCloseInit3:
		p.SendCloseInitCommand(cmd.ClientID, 3)

	case CmdSend:
		p.SendSendCommand(cmd.ClientID, cmd.DataBuf)

	case CmdSendInit1:
		p.SendSendInitCommand(cmd.ClientID, 1, cmd.DataBuf)

	case CmdSendInit2:
		p.SendSendInitCommand(cmd.ClientID, 2, cmd.DataBuf)

	case CmdSendInit3:
		p.SendSendInitCommand(cmd.ClientID, 3, cmd.DataBuf)

	case CmdSendInit4:
		p.SendSendInitCommand(cmd.ClientID, 4, cmd.DataBuf)
	}
}

func (p *Proxy) Close() {
	p.running.Store(false)
	p.closeOnce.Do(func() {
		close(p.stopCh)
	})

	if p.listener != nil {
		p.listener.Close()
	}

	p.serverConnMu.Lock()
	if p.serverConn != nil {
		p.serverConn.Close()
		p.serverConn = nil
	}
	p.serverConnMu.Unlock()
}

// ---- tcpserver.go ----
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
	clientsMu    lockhub.RWLocker
	onMessage    func(clientID string, data []byte)
	onClose      func(clientID string)
	running      atomic.Bool
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
	s.running.Store(true)

	go s.acceptLoop()
	return nil
}

func (s *TCPServer) acceptLoop() {
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
		if _, err := c.Conn.Write(data); err != nil {
			fmt.Printf("[TCPServer] broadcast write failed addr=%s uid=%s err=%v\n", c.Addr, c.UID, err)
		}
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
	if !s.running.Swap(false) {
		return nil
	}
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
	return s.running.Load()
}
