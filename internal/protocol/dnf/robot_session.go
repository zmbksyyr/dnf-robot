package dnf

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"sync/atomic"
	"time"

	"robot/internal/foundation/lockhub"
	"robot/internal/protocol/dnf/crypt"
)

const (
	robotSocketReadBufferSize = 32 * 1024
	robotSocketWriteTimeout   = 2 * time.Second
)

type robotDialFunc func(gameIP string, gamePort int, localIP string) (net.Conn, error)

type ClientState int

const (
	StateStop  ClientState = 0
	StateInit  ClientState = 1
	StateLogin ClientState = 2
	StateRun   ClientState = 3
	StateClean ClientState = 4
	StateWrong ClientState = 5
)

type DisconnectReason int

const (
	NoDisconnect  DisconnectReason = 0
	BadTokenOrCid DisconnectReason = 10
	BadUid        DisconnectReason = 11
	RepeatLogin   DisconnectReason = 12
	PunishReason  DisconnectReason = 26
)

type ClientError int

const (
	NoneError         ClientError = 0
	ConnectError      ClientError = 1
	StartRecvError    ClientError = 2
	CloseError        ClientError = 3
	LoginSendError    ClientError = 4
	MaxReconnectError ClientError = 5
	NormalDropError   ClientError = 6
	BadTokenError     ClientError = 7
	BadCidError       ClientError = 8
	BadUidError       ClientError = 9
	RepeatLoginError  ClientError = 10
	PunishReasonError ClientError = 11
)

type UserLoginInfo struct {
	IP        string
	Port      int
	Delay     uint32
	Token     [512]byte
	TokenSize uint32
	UID       uint32
	CID       int
	MaxReConn uint32
	ReDelay   uint32
	BirthPos  [4]uint32
}

type RobotVo struct {
	UID       uint32
	CID       int
	LoginIP   string
	LoginPort int
	LocalIP   string
	Conn      net.Conn
	Cipher    *crypt.DNFCipher
	State     ClientState
	LastError ClientError

	IsRefishUser      int
	Delay             uint32
	Token             [512]byte
	TokenSize         uint32
	MaxReConn         uint32
	ConnCount         uint32
	ReDelay           uint32
	PacketID          uint32
	CurVillage        uint8
	CurArea           uint8
	CurX              uint16
	CurY              uint16
	IsTokenRight      bool
	NccSent           bool
	SelectCharacSent  bool
	RunStartTime      uint32
	IsWaitingItemList bool
	MoveType          uint8
	DisconReason      DisconnectReason
	RobotType         string
	IP                string
	Port              int
	RobotTyp          int
	LastTradeID       uint16
	LastGMID          int
	WaitingMsgType    int
	LastTradeState    bool
	TradeMoney        uint32
	tradeQuoteLoading bool
	tradeQuotePending bool
	tradeQuoteVersion uint64

	TransactionArr [24]*Transaction
	InfanMap       map[int]Transaction

	PendingStoreTitle         string
	StoreDisplaySent          bool
	StoreDisplayAck           bool
	StoreDisplayRejected      bool
	StoreCreateRejected       bool
	LastStoreError            byte
	StoreCreated              bool
	PrepareStoreAfterItemList bool
	storeInventoryVersion     uint64
	AfterRunAsyncTaskVec      []AsyncTask
	LoginInfo                 UserLoginInfo

	DisjointCreateSent   bool
	DisjointDirectAck    bool
	DisjointActive       bool
	LastDisjointError    byte
	partyOptionReady     bool
	partyOptionSent      bool
	partyOptionData      [gameEtcOptionSize]byte
	partyRecvSource      recvBodySource
	natInfoSent          bool
	partySelfPeer        partyIPPeer
	partyPeers           [4]partyIPPeer
	partyPendingPeer     uint16
	partyPendingUntil    time.Time
	townEntityPositions  map[uint16]townEntityPosition
	townEntitySweepAt    time.Time
	partyUDPConn         *net.UDPConn
	partyUDPRunning      bool
	partyUDPGeneration   uint64
	partyRelayConn       net.Conn
	partyRelayConnecting bool
	partyRelayGeneration uint64
	partyRelayAt         time.Time
	partyRelayDial       partyRelayDialFunc
	partyRelayWriter     *partyRelayWriter
	partyTQOSSeq         [4][3]uint32
	partyTQOSReliableSeq [4][3]uint32
	partyTQOSReplies     [4][3]partyTQOSReliableReply
	partyTQOSReceived    [4][3]partyTQOSReceiveWindow
	partyTQOSEpochs      [4][3]partyTQOSEpoch
	partyTQOSCodecs      [4][3]partyTQOSCodec
	partyTQOSCodecKnown  [4][3]bool
	partyRobotProbeAt    [4]time.Time
	partyRobotProbeCount [4]uint8
	partyRobotPeerReady  [4]bool
	partyPeerRoute       [4]byte
	partyPeerRouteAt     [4]time.Time
	partyUDPDiagAt       time.Time
	partyDungeonFollow   []partyDungeonFollowPending
	partyDungeonLastAt   time.Time
	partyDungeonFlags    byte
	partySkillNextAt     time.Time
	partySkillRecoverAt  time.Time
	partySkillLoaded     bool
	partySkillLoading    bool
	partySkillGeneration uint64
	partySkillJob        int
	partySkillCandidates []partySkillCandidate
	partySkillLoad       partySkillProfileLoadFunc

	GMName [5][100]byte

	mu     lockhub.Locker
	sendMu lockhub.Locker

	lastSnapshot atomic.Pointer[RobotSnapshot]

	DB *sql.DB

	recvBuffer    []byte
	recvSize      int
	recvMaxSize   int
	minBufferSize int

	loginReal    [512]byte
	loginEnd     [64]byte
	selectCharac [16]byte
	setPos       [8]byte
	setArea      [16]byte
	setPosStart  [8]byte

	Controller      *RobotDnfTask
	connectInFlight bool

	done chan struct{}
}

type RobotSnapshot struct {
	UID                  uint32
	CID                  int
	State                ClientState
	LastError            ClientError
	DisconnectReason     DisconnectReason
	Reconnects           uint32
	RunStartTime         uint32
	RobotType            int
	StoreDisplaySent     bool
	StoreDisplayAck      bool
	StoreDisplayRejected bool
	StoreCreateRejected  bool
	LastStoreError       byte
	StoreCreated         bool
	DisjointCreateSent   bool
	DisjointDirectAck    bool
	DisjointActive       bool
	LastDisjointError    byte
	PartyActive          bool
	Village              uint8
	Area                 uint8
	X                    uint16
	Y                    uint16
}

func NewRobotVo(db *sql.DB) *RobotVo {
	r := &RobotVo{
		State:             StateStop,
		LastError:         NoneError,
		DisconReason:      NoDisconnect,
		WaitingMsgType:    33,
		minBufferSize:     4096,
		done:              make(chan struct{}),
		IsRefishUser:      0,
		SelectCharacSent:  false,
		NccSent:           false,
		IsWaitingItemList: false,
		IsTokenRight:      false,
		LastTradeState:    false,
		LastGMID:          0,
		LastTradeID:       0,
		TradeMoney:        0,
		RobotTyp:          0,
		DB:                db,
		InfanMap:          make(map[int]Transaction),
	}

	copy(r.GMName[0][:], []byte("GMUSER1"))
	copy(r.GMName[1][:], []byte("GMUSER2"))
	copy(r.GMName[2][:], []byte("GMUSER3"))
	copy(r.GMName[3][:], []byte("GMUSER4"))
	copy(r.GMName[4][:], []byte("GMUSER5"))

	tmpEnd := [64]byte{
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x08, 0x00, 0x00, 0x00, 0xF2, 0x03, 0x22,
		0xCF, 0x10, 0x4E, 0x91, 0xD0, 0x22, 0x67, 0x32,
		0x01, 0xC9, 0x3F, 0xAB, 0x01, 0x03, 0x00, 0x01,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
	copy(r.loginEnd[:], tmpEnd[:])

	tmpPos := [8]byte{0xDA, 0x01, 0xEA, 0x00, 0x05, 0x68, 0x00, 0x00}
	copy(r.setPos[:], tmpPos[:])

	tmpArea := [16]byte{0x01, 0x00, 0x11, 0x03, 0x9E, 0x00, 0x05, 0x01,
		0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	copy(r.setArea[:], tmpArea[:])

	tmpPosStart := [8]byte{0x11, 0x03, 0x00, 0x01, 0x05, 0x64, 0x00, 0x00}
	copy(r.setPosStart[:], tmpPosStart[:])

	return r
}

func (r *RobotVo) Snapshot() RobotSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.publishSnapshotUnsafe()
}

func (r *RobotVo) TrySnapshot() (RobotSnapshot, bool) {
	if r.mu.TryLock() {
		snapshot := r.publishSnapshotUnsafe()
		r.mu.Unlock()
		return snapshot, true
	}
	if snapshot := r.lastSnapshot.Load(); snapshot != nil {
		return *snapshot, false
	}
	return RobotSnapshot{}, false
}

func (r *RobotVo) publishSnapshotUnsafe() RobotSnapshot {
	snapshot := RobotSnapshot{
		UID:                  r.UID,
		CID:                  r.CID,
		State:                r.State,
		LastError:            r.LastError,
		DisconnectReason:     r.DisconReason,
		Reconnects:           r.ConnCount,
		RunStartTime:         r.RunStartTime,
		RobotType:            r.RobotTyp,
		StoreDisplaySent:     r.StoreDisplaySent,
		StoreDisplayAck:      r.StoreDisplayAck,
		StoreDisplayRejected: r.StoreDisplayRejected,
		StoreCreateRejected:  r.StoreCreateRejected,
		LastStoreError:       r.LastStoreError,
		StoreCreated:         r.StoreCreated,
		DisjointCreateSent:   r.DisjointCreateSent,
		DisjointDirectAck:    r.DisjointDirectAck,
		DisjointActive:       r.DisjointActive,
		LastDisjointError:    r.LastDisjointError,
		PartyActive:          r.partyActiveUnsafe(),
		Village:              r.CurVillage,
		Area:                 r.CurArea,
		X:                    r.CurX,
		Y:                    r.CurY,
	}
	r.lastSnapshot.Store(&snapshot)
	return snapshot
}

func (r *RobotVo) Load(info UserLoginInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.LoginInfo = info
	r.IP = info.IP
	r.Port = info.Port
	r.Delay = info.Delay
	r.UID = info.UID
	r.CID = info.CID
	r.MaxReConn = info.MaxReConn
	r.ReDelay = info.ReDelay
	r.TokenSize = info.TokenSize
	if r.TokenSize > 512 {
		r.TokenSize = 512
	}
	copy(r.Token[:], info.Token[:r.TokenSize])
	r.CurVillage = uint8(info.BirthPos[0])
	r.CurArea = uint8(info.BirthPos[1])
	r.CurX = uint16(info.BirthPos[2])
	r.CurY = uint16(info.BirthPos[3])
	r.Cipher = crypt.NewDNFCipher()
	r.MoveType = 5
	r.StoreDisplaySent = false
	r.StoreDisplayAck = false
	r.StoreDisplayRejected = false
	r.StoreCreateRejected = false
	r.LastStoreError = 0
	r.StoreCreated = false
	r.PrepareStoreAfterItemList = false
	r.storeInventoryVersion++
	r.invalidateTradeQuoteUnsafe()
	r.clearTradeUnsafe()
	r.DisjointCreateSent = false
	r.DisjointDirectAck = false
	r.DisjointActive = false
	r.LastDisjointError = 0
	copy(r.partyOptionData[:], defaultPartyAcceptGameOptions())
	r.partyOptionReady = true
	r.partyOptionSent = false
	r.partyRecvSource = recvBodySourceUnknown
	r.natInfoSent = false
	r.partySelfPeer = partyIPPeer{}
	r.partyPeers = [4]partyIPPeer{}
	r.clearPartyPendingUnsafe()
	r.townEntityPositions = make(map[uint16]townEntityPosition)
	r.townEntitySweepAt = time.Time{}
	r.partyRelayAt = time.Time{}
	r.resetPartyTQOSTransportUnsafe()
	r.resetPartySkillProfileUnsafe()
	r.closePartyUDPUnsafe()
	r.closePartyRelayUnsafe()
	r.recvSize = 0
	r.Conn = nil
	r.LoginIP = info.IP
	r.LoginPort = info.Port
	r.LocalIP = "127.0.0.1"
	r.publishSnapshotUnsafe()
}

func (r *RobotVo) CloseOut() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeOutUnsafe()
}

func (r *RobotVo) TryCloseOut() bool {
	if !r.mu.TryLock() {
		return false
	}
	defer r.mu.Unlock()
	r.closeOutUnsafe()
	return true
}

func (r *RobotVo) closeOutUnsafe() {
	if r.Conn != nil {
		r.Conn.Close()
		r.Conn = nil
	}
	r.closePartyUDPUnsafe()
	r.closePartyRelayUnsafe()
	r.recvBuffer = nil
	r.recvSize = 0
	r.invalidateTradeQuoteUnsafe()
	r.clearTradeUnsafe()
	r.State = StateStop
	r.publishSnapshotUnsafe()
}

func (r *RobotVo) prepareConnect(controller *RobotDnfTask) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if controller == nil || r.State != StateStop {
		return false
	}
	r.Controller = controller
	r.IsTokenRight = false
	r.connectInFlight = false
	r.State = StateInit
	r.publishSnapshotUnsafe()
	return true
}

func (r *RobotVo) readyToConnect() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.State == StateInit && !r.connectInFlight
}

func (r *RobotVo) Connect() {
	r.connect(dialRobot)
}

func (r *RobotVo) connect(dial robotDialFunc) {
	if dial == nil {
		return
	}
	r.mu.Lock()
	if r.State != StateInit || r.connectInFlight {
		r.mu.Unlock()
		return
	}
	r.connectInFlight = true
	gameIP := r.IP
	gamePort := r.Port
	localIP := r.LocalIP
	controller := r.Controller
	r.mu.Unlock()
	if controller == nil || !controller.isCurrent(r.UID, r) {
		r.mu.Lock()
		r.connectInFlight = false
		r.mu.Unlock()
		return
	}
	if gameIP == "" || gameIP == "127.0.0.1" {
		gameIP = "10.0.0.1"
	}

	conn, err := dial(gameIP, gamePort, localIP)
	addr := net.JoinHostPort(gameIP, strconv.Itoa(gamePort))
	if err != nil {
		fmt.Printf("[RobotVo] connect failed uid=%d addr=%s err=%v\n", r.UID, addr, err)
		r.mu.Lock()
		r.connectInFlight = false
		r.mu.Unlock()
		r.RefishConnect()
		return
	}
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetKeepAlive(true)
		_ = tcpConn.SetKeepAlivePeriod(30 * time.Second)
	}

	r.mu.Lock()
	r.connectInFlight = false
	if r.State != StateInit || r.Controller != controller {
		r.mu.Unlock()
		_ = conn.Close()
		return
	}
	if gameIP != localIP {
		r.RobotTyp = 1
	}
	r.State = StateLogin
	r.DisconReason = NoDisconnect
	r.minBufferSize = 4096
	r.recvSize = 0
	r.recvMaxSize = 4096
	r.recvBuffer = make([]byte, r.recvMaxSize)
	r.Conn = conn
	r.publishSnapshotUnsafe()
	r.mu.Unlock()

	go r.readLoop(conn)
}

func dialRobot(gameIP string, gamePort int, _ string) (net.Conn, error) {
	addr := net.JoinHostPort(gameIP, strconv.Itoa(gamePort))
	var d net.Dialer
	d.Timeout = 10 * time.Second
	if localIPAvailable(gameIP) {
		tcpAddr, err := net.ResolveTCPAddr("tcp", net.JoinHostPort(gameIP, "0"))
		if err == nil {
			d.LocalAddr = tcpAddr
		}
	}
	return d.Dial("tcp", addr)
}

func localIPAvailable(ip string) bool {
	if ip == "" {
		return false
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if ok && ipnet.IP.String() == ip {
			return true
		}
	}
	return false
}

func (r *RobotVo) readLoop(conn net.Conn) {
	defer func() {
		if rec := recover(); rec != nil {
			fmt.Printf("[RobotVo] readLoop panic uid=%d err=%v\n", r.UID, rec)
		}
		r.mu.Lock()
		r.closePartyUDPUnsafe()
		r.closePartyRelayUnsafe()
		if r.State != StateStop {
			r.State = StateStop
		}
		r.mu.Unlock()
	}()
	buf := make([]byte, robotSocketReadBufferSize)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			r.mu.Lock()
			if r.State == StateStop {
				r.mu.Unlock()
				return
			}
			r.LastError = StartRecvError
			if r.Conn != nil {
				r.Conn.Close()
				r.Conn = nil
			}
			r.closePartyUDPUnsafe()
			r.closePartyRelayUnsafe()
			shouldReconnect := r.Controller != nil && r.ConnCount < r.MaxReConn
			if !shouldReconnect {
				r.State = StateStop
				controller := r.Controller
				r.mu.Unlock()
				if controller != nil {
					controller.DeleteIf(r.UID, r)
				}
				return
			}
			r.mu.Unlock()
			r.RefishConnect()
			return
		}
		if n > 0 {
			r.onRecvData(buf[:n])
		}
	}
}

func (r *RobotVo) onRecvData(data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State == StateStop || len(data) < 1 {
		return
	}

	if r.recvMaxSize-r.recvSize < len(data) {
		for r.recvMaxSize-r.recvSize < len(data) {
			r.recvMaxSize *= 2
		}
		newBuf := make([]byte, r.recvMaxSize)
		copy(newBuf, r.recvBuffer[:r.recvSize])
		r.recvBuffer = newBuf
	}

	copy(r.recvBuffer[r.recvSize:], data)
	r.recvSize += len(data)

	consumed := 0
	for r.recvSize-consumed >= 7 {
		packet := r.recvBuffer[consumed:r.recvSize]
		packetFlag := packet[0]
		packetType := binary.LittleEndian.Uint16(packet[1:3])
		packetSize := binary.LittleEndian.Uint32(packet[3:7])
		const maxPacketSize uint32 = 1024 * 1024
		if packetSize < 7 || packetSize > maxPacketSize {
			fmt.Printf("[RobotVo] invalid packet uid=%d flag=%d type=%d size=%d recvSize=%d\n",
				r.UID, packetFlag, packetType, packetSize, r.recvSize-consumed)
			r.State = StateStop
			if r.Conn != nil {
				r.Conn.Close()
				r.Conn = nil
			}
			return
		}
		packetBytes := int(packetSize)
		if len(packet) < packetBytes {
			break
		}
		r.parsePacket(packet[:packetBytes])
		consumed += packetBytes
	}

	if consumed > 0 {
		copy(r.recvBuffer, r.recvBuffer[consumed:r.recvSize])
		r.recvSize -= consumed
	}

	minBufferSize := r.minBufferSize
	if minBufferSize <= 0 {
		minBufferSize = 4096
	}
	if r.recvMaxSize > minBufferSize && r.recvSize < r.recvMaxSize/4 {
		newSize := r.recvMaxSize
		for r.recvSize < newSize/4 && newSize > minBufferSize {
			newSize /= 2
		}
		if newSize < minBufferSize {
			newSize = minBufferSize
		}
		if newSize != r.recvMaxSize {
			newBuf := make([]byte, newSize)
			copy(newBuf, r.recvBuffer[:r.recvSize])
			r.recvBuffer = newBuf
			r.recvMaxSize = newSize
		}
	}
}

func (r *RobotVo) sendRaw(pkt []byte) bool {
	r.sendMu.Lock()
	defer r.sendMu.Unlock()

	conn := r.Conn
	if conn == nil || len(pkt) == 0 {
		return false
	}
	for written := 0; written < len(pkt); {
		if err := conn.SetWriteDeadline(time.Now().Add(robotSocketWriteTimeout)); err != nil {
			fmt.Printf("[RobotVo] set write deadline failed uid=%d err=%v\n", r.UID, err)
			_ = conn.Close()
			return false
		}
		n, err := conn.Write(pkt[written:])
		if err != nil {
			fmt.Printf("[RobotVo] write failed uid=%d err=%v\n", r.UID, err)
			_ = conn.Close()
			return false
		}
		if n == 0 {
			fmt.Printf("[RobotVo] write made no progress uid=%d\n", r.UID)
			_ = conn.Close()
			return false
		}
		written += n
	}
	return true
}
