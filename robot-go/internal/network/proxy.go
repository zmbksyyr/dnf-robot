package network

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

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
	serverConnMu sync.Mutex

	beatCount   uint32
	beatCountMu sync.Mutex

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
		p.serverConn = conn
		p.serverConnMu.Unlock()

		go p.clientWorkThread()
		go p.clientBeatThread()

		break
	}
}

func (p *Proxy) clientWorkThread() {
	buf := make([]byte, 1024)
	for p.running.Load() {
		p.serverConnMu.Lock()
		conn := p.serverConn
		p.serverConnMu.Unlock()

		if conn == nil {
			return
		}

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
			p.serverSocketSend(header)

		case <-p.stopCh:
			return
		}
	}
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
