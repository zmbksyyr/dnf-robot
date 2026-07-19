package dnf

import (
	"bytes"
	"compress/zlib"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"math/bits"
	"net"
	"os"
	"path/filepath"
	"robot/internal/foundation/lockhub"
	foundationlog "robot/internal/foundation/log"
	sqlpkg "robot/internal/foundation/sql"
	"robot/internal/protocol/dnf/crypt"
	"robot/internal/shared"
	"sort"
	"strconv"
	"time"
)

// ---- robot.go ----
var partyRelayPort = 7200

func ConfigurePartyRelayPort(port int) {
	if port <= 0 || port > 65535 {
		port = 7200
	}
	partyRelayPort = port
}

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

type AsyncTaskType int

const (
	AsyncMove     AsyncTaskType = 0
	AsyncDisjoint AsyncTaskType = 1
	AsyncPriStore AsyncTaskType = 2
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

type StoreInfo struct {
	Index    int
	BoxType  int
	BoxIndex int
	Price    int
	Count    int
}

type AsyncTask struct {
	Type      AsyncTaskType
	Village   int
	Area      int
	X         int
	Y         int
	Mtype     int
	Speed     int
	Cost      int
	Title     string
	StoreInfo []StoreInfo
}

type Transaction struct {
	ItemPos  int16
	ItemId   int32
	ItemNum  int32
	ItemType int32
}

type ShopVo struct {
	TradeItem  int
	Price      int
	ItemNumber int
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
	AfterRunAsyncTaskVec      []AsyncTask
	LoginInfo                 UserLoginInfo

	DisjointCreateSent   bool
	DisjointDirectAck    bool
	DisjointActive       bool
	LastDisjointError    byte
	partyOptionReady     bool
	partyOptionSent      bool
	partyOptionData      [gameEtcOptionSize]byte
	natInfoSent          bool
	partySelfPeer        partyIPPeer
	partyPeers           [4]partyIPPeer
	partyPendingPeer     uint16
	partyPendingUntil    time.Time
	townEntityPositions  map[uint16]townEntityPosition
	partyUDPConn         *net.UDPConn
	partyRelayConn       net.Conn
	partyRelayAt         time.Time
	partyTQOSSeq         [4][3]uint32
	partyTQOSReliableSeq [4][3]uint32
	partyTQOSCodecs      [4][3]partyTQOSCodec
	partyTQOSCodecKnown  [4][3]bool
	partyRobotProbeAt    [4]time.Time
	partyRobotProbeCount [4]uint8
	partyRobotPeerReady  [4]bool
	partyUDPDiagAt       time.Time
	partyDungeonTraceAt  time.Time
	partyDungeonFollow   []partyDungeonFollowPending
	partyDungeonLastAt   time.Time
	partyDungeonFlags    byte
	partySkillNextAt     time.Time
	partySkillRecoverAt  time.Time
	partySkillLoaded     bool
	partySkillJob        int
	partySkillCandidates []partySkillCandidate

	GMName [5][100]byte

	mu     lockhub.Locker
	sendMu lockhub.Locker

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

	Controller Controller

	done chan struct{}
}

type Controller interface {
	Insert(uid uint32, vo *RobotVo)
	Delete(uid uint32)
	AddMessage(msgType string, data interface{})
	AddMessageDelay(msgType string, data interface{}, delaySec int)
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
	return RobotSnapshot{
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
}

func (r *RobotVo) ResetPrivateStoreState() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.RobotTyp == 2 {
		r.RobotTyp = 0
	}
	r.StoreDisplaySent = false
	r.StoreDisplayAck = false
	r.StoreDisplayRejected = false
	r.StoreCreateRejected = false
	r.LastStoreError = 0
	r.StoreCreated = false
	r.PrepareStoreAfterItemList = false
	r.PendingStoreTitle = ""
}

func (r *RobotVo) ResetDisjointStoreState() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.RobotTyp == 3 {
		r.RobotTyp = 0
	}
	r.DisjointCreateSent = false
	r.DisjointDirectAck = false
	r.DisjointActive = false
	r.LastDisjointError = 0
}

func (r *RobotVo) PreparePrivateStoreState(title string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.PendingStoreTitle = title
	r.StoreDisplaySent = false
	r.StoreDisplayAck = false
	r.StoreDisplayRejected = false
	r.StoreCreateRejected = false
	r.LastStoreError = 0
	r.StoreCreated = false
	r.PrepareStoreAfterItemList = false
	r.RobotTyp = 2
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
	r.DisjointCreateSent = false
	r.DisjointDirectAck = false
	r.DisjointActive = false
	r.LastDisjointError = 0
	copy(r.partyOptionData[:], defaultPartyAcceptGameOptions())
	r.partyOptionReady = true
	r.partyOptionSent = false
	r.natInfoSent = false
	r.partySelfPeer = partyIPPeer{}
	r.partyPeers = [4]partyIPPeer{}
	r.clearPartyPendingUnsafe()
	r.townEntityPositions = make(map[uint16]townEntityPosition)
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
}

func (r *RobotVo) CloseOut() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.Conn != nil {
		r.Conn.Close()
		r.Conn = nil
	}
	r.closePartyUDPUnsafe()
	r.closePartyRelayUnsafe()
	r.recvBuffer = nil
	r.recvSize = 0
	r.State = StateStop
}

func (r *RobotVo) CloseTaskVec() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.AfterRunAsyncTaskVec = nil
}

func (r *RobotVo) Connect() {
	r.mu.Lock()
	if r.State != StateStop {
		r.mu.Unlock()
		return
	}
	r.State = StateInit
	gameIP := r.IP
	gamePort := r.Port
	localIP := r.LocalIP
	r.mu.Unlock()

	if gameIP == "" || gameIP == "127.0.0.1" {
		gameIP = "10.0.0.1"
	}
	addr := net.JoinHostPort(gameIP, strconv.Itoa(gamePort))
	var d net.Dialer
	d.Timeout = 10 * time.Second
	if localIPAvailable(gameIP) {
		tcpAddr, err := net.ResolveTCPAddr("tcp", net.JoinHostPort(gameIP, "0"))
		if err == nil {
			d.LocalAddr = tcpAddr
		}
	}
	conn, err := d.Dial("tcp", addr)
	if err != nil {
		fmt.Printf("[RobotVo] connect failed uid=%d addr=%s err=%v\n", r.UID, addr, err)
		r.RefishConnect()
		return
	}
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetKeepAlive(true)
		_ = tcpConn.SetKeepAlivePeriod(30 * time.Second)
	}

	r.mu.Lock()
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
	if r.Controller != nil {
		r.Controller.Insert(r.UID, r)
	}
	r.mu.Unlock()

	go r.readLoop(conn)
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
	buf := make([]byte, 65536)
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
			reDelay := r.ReDelay
			if !shouldReconnect {
				r.State = StateStop
				if r.Controller != nil {
					r.Controller.Delete(r.UID)
				}
				r.mu.Unlock()
				return
			}
			r.mu.Unlock()
			if reDelay > 0 {
				time.Sleep(time.Duration(reDelay) * time.Millisecond)
			}
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

	for r.recvSize >= 7 {
		if r.recvSize < 7 {
			break
		}
		packetFlag := r.recvBuffer[0]
		packetType := binary.LittleEndian.Uint16(r.recvBuffer[1:3])
		packetSize := binary.LittleEndian.Uint32(r.recvBuffer[3:7])
		const maxPacketSize uint32 = 1024 * 1024
		if packetSize < 7 || packetSize > maxPacketSize {
			fmt.Printf("[RobotVo] invalid packet uid=%d flag=%d type=%d size=%d recvSize=%d\n",
				r.UID, packetFlag, packetType, packetSize, r.recvSize)
			r.State = StateStop
			if r.Conn != nil {
				r.Conn.Close()
				r.Conn = nil
			}
			return
		}
		if uint32(r.recvSize) >= packetSize {
			if uint32(r.recvSize) <= uint32(r.recvMaxSize) {
				r.parsePacket(r.recvBuffer[:packetSize])
			} else {
				r.recvBuffer = nil
				r.State = StateStop
				if r.Conn != nil {
					r.Conn.Close()
					r.Conn = nil
				}
				return
			}
			if uint32(r.recvSize) > packetSize {
				copy(r.recvBuffer, r.recvBuffer[packetSize:r.recvSize])
			}
			r.recvSize = int(uint32(r.recvSize) - packetSize)
		} else {
			break
		}
	}

	if r.recvSize < r.recvMaxSize/4 {
		for r.recvSize < r.recvMaxSize/4 && r.recvMaxSize > 4096 {
			r.recvMaxSize /= 2
		}
		newBuf := make([]byte, r.recvMaxSize)
		copy(newBuf, r.recvBuffer[:r.recvSize])
		r.recvBuffer = newBuf
	}
}

func (r *RobotVo) parsePacket(inBuf []byte) {
	if r.State == StateStop {
		return
	}
	if len(inBuf) < 7 {
		return
	}

	pInBuf := inBuf
	dInSize := len(inBuf)
	packetFlag := pInBuf[0]
	packetType := binary.LittleEndian.Uint16(pInBuf[1:3])
	_ = binary.LittleEndian.Uint32(pInBuf[3:7])
	isAnti := false

	if packetFlag == 0 && packetType == 561 && dInSize > 36 {
		dec, err := r.Cipher.DecryptAnti(pInBuf[19:])
		if err == nil && len(dec) >= 7 {
			pInBuf = dec
			dInSize = len(dec)
			packetFlag = pInBuf[0]
			packetType = binary.LittleEndian.Uint16(pInBuf[1:3])
			_ = binary.LittleEndian.Uint32(pInBuf[3:7])
			isAnti = true
		}
	}

	if r.State == StateRun && packetFlag == 0 && dInSize >= 15 && (packetType == 28 || packetType == 29) {
		pkt, err := buildSendPacket(40, uint16(r.PacketID), buildFinishLoadingPayload(0, 0), r.Cipher)
		r.PacketID++
		if err != nil {
			fmt.Printf("[DUNGEON_FINISH_LOADING_BUILD_ERROR] uid=%d source_type=%d err=%v\n", r.UID, packetType, err)
		} else if !r.sendRaw(pkt) {
			fmt.Printf("[DUNGEON_FINISH_LOADING_SEND_ERROR] uid=%d source_type=%d\n", r.UID, packetType)
		}
		return
	}

	if r.State == StateRun && packetFlag == 0 && packetType == 22 {
		_, _, decData, err := parseRecvPacket(r.Cipher, pInBuf, isAnti)
		if err == nil {
			if position, ok := r.rememberTownEntityUnsafe(decData); ok {
				r.followPartyLeaderTownPositionUnsafe(position)
			}
		}
		return
	}

	if r.State == StateRun && packetFlag == 0 && packetType == 23 {
		_, _, decData, err := parseRecvPacket(r.Cipher, pInBuf, isAnti)
		if err == nil {
			if area, ok := parseTownEntityArea(decData); ok {
				r.followPartyLeaderTownAreaUnsafe(area)
			}
		}
		return
	}

	if r.State == StateRun && packetFlag == 0 && packetType == 9 && len(pInBuf) > 15 {
		if decData, err := inflatePartyInfo(pInBuf[15:]); err == nil && partyInfoClearsParty(decData) {
			r.clearPartyUnsafe()
		}
		return
	}

	if r.State == StateRun && packetFlag == 0 && packetType == 11 {
		_, _, decData, err := parseRecvPacket(r.Cipher, pInBuf, isAnti)
		if err == nil {
			if self, peers, ok := parsePartyIPInfoSnapshot(decData, uint32(r.UID)); ok {
				tracePartyIPInfo(r.UID, self, peers)
				r.partySelfPeer = self
				r.setPartyPeersUnsafe(peers)
				r.ensurePartyRelayUnsafe()
				r.followCachedPartyLeaderTownPositionUnsafe()
				r.startPartyRobotPeerNegotiationUnsafe()
			}
		}
		return
	}

	if r.State == StateRun && packetFlag == 0 && packetType == 6 {
		_, _, decData, err := parseRecvPacket(r.Cipher, pInBuf, isAnti)
		if err == nil && len(decData) >= 2 {
			uniqueID := binary.LittleEndian.Uint16(decData[:2])
			delete(r.townEntityPositions, uniqueID)
		}
		return
	}

	// Handle flag=1 (encrypted server-to-client) packets
	if packetFlag == 1 && packetType == 1 && r.State == StateLogin {
		if dInSize < 15 {
			fmt.Printf("[RobotVo] short encrypted packet uid=%d size=%d\n", r.UID, dInSize)
			r.State = StateStop
			if r.Conn != nil {
				r.Conn.Close()
				r.Conn = nil
			}
			return
		}
		encryptedData := pInBuf[15:]
		_, _ = r.Cipher.DecryptLogin(encryptedData)
	}

	if packetFlag == 0 && packetType == 272 && r.State == StateLogin && !r.NccSent {
		_, _, decData, err := parseRecvPacket(r.Cipher, pInBuf, isAnti)
		if err == nil && len(decData) > 0 {
			r.NccSent = true
			// Send NCC response (type=294) first
			pkt, err := buildSendPacket(294, 0, decData, r.Cipher)
			if err == nil {
				r.sendRaw(pkt)
			}
			r.sendSelectCharacUnsafe("after type=272")
		}
	}

	if packetFlag == 0 && packetType == 0 {
		var setPos [8]byte
		pkt, err := buildSendPacket(1, uint16(r.PacketID), setPos[:], r.Cipher)
		r.PacketID++
		if err == nil {
			r.sendRaw(pkt)
		}
	}

	if packetFlag == 0 && packetType == 199 {
		_, _, decData, err := parseRecvPacket(r.Cipher, pInBuf, isAnti)
		if err == nil && len(decData) >= 4 {
			r.DisconReason = DisconnectReason(binary.LittleEndian.Uint32(decData[0:4]))
			if r.DisconReason != NoDisconnect {
				go r.RefishConnect()
			}
		}
		return
	}

	if packetFlag == 0 && packetType == 173 && (r.State == StateLogin || r.State == StateRun) {
		_, _, decData, err := parseRecvPacket(r.Cipher, pInBuf, isAnti)
		if err == nil {
			optionData, ok := partyAcceptGameOptions(decData)
			if ok {
				copy(r.partyOptionData[:], optionData)
				r.partyOptionReady = true
				r.partyOptionSent = false
				r.sendPartyOptionUnsafe()
			}
		}
		return
	}

	if r.State == StateRun && packetFlag == 1 && packetType == 238 && r.RobotTyp == 3 {
		_, _, decData, err := parseRecvPacket(r.Cipher, pInBuf, isAnti)
		if err == nil {
			if len(decData) > 0 && decData[0] == 1 {
				r.DisjointDirectAck = true
				r.DisjointActive = true
				r.LastDisjointError = 0
				fmt.Printf("[DISJOINT_DIRECT_ACK] uid=%d payload=%x\n", r.UID, decData)
			} else if len(decData) >= 2 && decData[0] == 0 {
				r.DisjointDirectAck = false
				r.DisjointActive = false
				r.LastDisjointError = decData[1]
				fmt.Printf("[DISJOINT_238_ERROR] uid=%d error=%d payload=%x\n", r.UID, r.LastDisjointError, decData)
			}
		}
		return
	}

	if r.State == StateRun && (packetType == 88 || packetType == 90) && r.RobotTyp == 2 {
		_, _, decData, err := parseRecvPacket(r.Cipher, pInBuf, isAnti)
		value := byte(0)
		if len(decData) > 0 {
			value = decData[0]
		}
		if packetFlag == 1 && packetType == 88 && err == nil && value == 1 {
			r.StoreCreated = true
		}
		if packetFlag == 1 && packetType == 88 && err == nil && value == 0 {
			r.StoreCreateRejected = true
		}
		if packetFlag == 1 && packetType == 90 && err == nil && value == 1 {
			r.StoreDisplayAck = true
		}
		storeErr := byte(0)
		if len(decData) > 1 {
			storeErr = decData[1]
		}
		if packetFlag == 1 && err == nil && value == 0 && storeErr != 0 {
			r.LastStoreError = storeErr
		}
		if packetFlag == 1 && packetType == 90 && err == nil && value == 0 && storeErr == 0x11 {
			r.StoreDisplayRejected = true
		}
	}

	if packetFlag == 0 && packetType == 13 && (r.State == StateRun || r.State == StateLogin) {
		wasWaiting := r.IsWaitingItemList
		r.IsWaitingItemList = false
		for k := range r.InfanMap {
			delete(r.InfanMap, k)
		}
		_, _, decData, err := parseRecvPacket(r.Cipher, pInBuf, isAnti)
		if err == nil && len(decData) >= 5 {
			itemNumber := binary.LittleEndian.Uint16(decData[3:5])
			pBuf := decData[5:]
			for i := uint16(0); i < itemNumber; i++ {
				if len(pBuf) < 25 {
					break
				}
				itemID := int32(binary.LittleEndian.Uint32(pBuf[2:6]))
				itemPos := int16(binary.LittleEndian.Uint16(pBuf[0:2]))
				itemNum := int32(binary.LittleEndian.Uint32(pBuf[6:10]))
				pBuf = pBuf[25:]
				r.InfanMap[int(itemID)] = Transaction{ItemPos: itemPos, ItemId: itemID, ItemNum: itemNum}
			}
		}
		if wasWaiting {
			if r.PrepareStoreAfterItemList {
				r.PrepareStoreAfterItemList = false
				go func() {
					r.CreatePrivateStore()
					deadline := time.Now().Add(4 * time.Second)
					for time.Now().Before(deadline) {
						snap := r.Snapshot()
						if snap.StoreCreated || snap.State != StateRun {
							break
						}
						time.Sleep(100 * time.Millisecond)
					}
					if snap := r.Snapshot(); snap.State == StateRun {
						r.GetDbDataAndCompleteDisplay()
					}
				}()
			} else {
				r.getDbDataAndCompleteDisplayUnsafe()
			}
		}
		return
	}

	if packetFlag == 0 && packetType == 15 && r.State == StateRun {
		_, _, decData, err := parseRecvPacket(r.Cipher, pInBuf, isAnti)
		if err == nil && len(decData) >= 15 {
			if r.StoreDisplaySent && r.RobotTyp == 2 && !r.StoreDisplayAck {
				r.StoreDisplayAck = true
			}
			itemPos := int16(binary.LittleEndian.Uint16(decData[0:2]))
			itemID := int32(binary.LittleEndian.Uint32(decData[2:6]))
			itemNum := int32(binary.LittleEndian.Uint32(decData[6:10]))
			itemType := int32(binary.LittleEndian.Uint32(decData[11:15]))

			idx := int(itemPos) - 3
			if itemID < 0 {
				if idx >= 0 && idx < 24 {
					r.TransactionArr[idx] = nil
				}
			} else {
				if idx >= 0 && idx < 24 {
					tx := &Transaction{ItemPos: itemPos - 3, ItemId: itemID, ItemNum: itemNum, ItemType: itemType}
					if itemType == 100 || tx.ItemNum < 1 {
						tx.ItemNum = 1
					}
					r.TransactionArr[idx] = tx
				}
			}

			shopVoMap := r.getShopVo(1)
			var money uint32
			if len(shopVoMap) > 0 {
				for i := 0; i < 24; i++ {
					if r.TransactionArr[i] != nil {
						tx := r.TransactionArr[i]
						if vo, ok := shopVoMap[int(tx.ItemId)]; ok && tx.ItemNum > 0 {
							itemPrice := uint32(tx.ItemNum) * uint32(vo.Price)
							if itemPrice > 0 {
								money += itemPrice
							}
						}
					}
				}
			}

			{
				var sendBuf [32]byte
				binary.LittleEndian.PutUint32(sendBuf[7:11], r.TradeMoney)
				sendBuf[0] = 4
				pkt, err := buildSendPacket(19, uint16(r.PacketID), sendBuf[:], r.Cipher)
				r.PacketID++
				if err == nil && !r.sendRaw(pkt) {
					r.TradeMoney = 0
					r.LastTradeState = false
					r.LastTradeID = 0
					for i := 0; i < 24; i++ {
						r.TransactionArr[i] = nil
					}
				}
			}

			{
				var sendBuf [32]byte
				binary.LittleEndian.PutUint32(sendBuf[7:11], money)
				sendBuf[11] = 4
				pkt, err := buildSendPacket(19, uint16(r.PacketID), sendBuf[:], r.Cipher)
				r.PacketID++
				if err == nil {
					if r.sendRaw(pkt) {
						r.TradeMoney = money
					} else {
						r.TradeMoney = 0
						r.LastTradeState = false
						r.LastTradeID = 0
						for i := 0; i < 24; i++ {
							r.TransactionArr[i] = nil
						}
					}
				}
			}
		}
		return
	}

	if packetFlag == 0 && packetType == 7 && r.State == StateRun {
		_, _, decData, err := parseRecvPacket(r.Cipher, pInBuf, isAnti)
		if err == nil {
			data, typ, ok := buildPeerResponse(decData)
			if ok && (typ == peerRequestParty || (!r.LastTradeState && r.LastTradeID == 0)) {
				uniqueID := binary.LittleEndian.Uint16(data[0:2])
				pkt, err := buildSendPacket(11, uint16(r.PacketID), data, r.Cipher)
				r.PacketID++
				if err != nil {
					fmt.Printf("[PEER_RESPONSE_BUILD_ERROR] uid=%d type=%d err=%v\n", r.UID, typ, err)
				}
				if err == nil {
					sent := r.sendRaw(pkt)
					if sent && typ == peerRequestTrade {
						r.LastTradeID = uniqueID
						r.LastTradeState = true
					}
					if sent && typ == peerRequestParty {
						fmt.Printf("[PARTY_AUTO_ACCEPT] uid=%d peer_unique_id=%d request_id=%d\n",
							r.UID, uniqueID, binary.LittleEndian.Uint32(data[3:7]))
						r.resetPartyTQOSTransportUnsafe()
						r.setPartyPendingUnsafe(uniqueID)
						r.ensurePartyRelayUnsafe()
					}
				}
				if typ == peerRequestTrade {
					r.TradeMoney = 0
				}
			}
		}
		return
	}

	if packetFlag == 0 && packetType == 16 && r.State == StateRun {
		r.TradeMoney = 0
		r.LastTradeState = false
		r.LastTradeID = 0
		for i := 0; i < 24; i++ {
			r.TransactionArr[i] = nil
		}
		return
	}

	if packetFlag == 0 && packetType == 17 && r.State == StateRun {
		_, _, decData, err := parseRecvPacket(r.Cipher, pInBuf, isAnti)
		if err == nil && len(decData) >= 3 {
			uniqueID := binary.LittleEndian.Uint16(decData[0:2])
			state := decData[2]
			if uniqueID == r.LastTradeID && state == 1 {
				var data [8]byte
				data[0] = 1
				pkt, err := buildSendPacket(26, uint16(r.PacketID), data[:], r.Cipher)
				r.PacketID++
				if err == nil {
					r.sendRaw(pkt)
				}
			}
			if uniqueID != r.LastTradeID && state == 1 {
				var data [8]byte
				data[0] = 3
				pkt, err := buildSendPacket(26, uint16(r.PacketID), data[:], r.Cipher)
				r.PacketID++
				if err == nil {
					r.sendRaw(pkt)
				}
			}
		}
		return
	}

	if packetFlag == 0 && packetType == 1 && r.State == StateLogin {
		_, _, decData, err := parseRecvPacket(r.Cipher, pInBuf, isAnti)
		if err == nil && len(decData) >= 334 {
			errInit := r.Cipher.Initialize(decData[:334])
			if errInit == nil {
				const loginBodySize = 400
				if 8+int(r.TokenSize) > loginBodySize {
					r.State = StateStop
					if r.Conn != nil {
						r.Conn.Close()
						r.Conn = nil
					}
					return
				}
				for i := 0; i < 4; i++ {
					r.loginReal[i] = 0
				}
				binary.LittleEndian.PutUint32(r.loginReal[4:8], r.TokenSize)
				copy(r.loginReal[8:], r.Token[:r.TokenSize])
				copy(r.loginReal[8+r.TokenSize:], r.loginEnd[:])
				var body [400]byte
				copy(body[:], r.loginReal[:loginBodySize])
				pkt, err := buildSendPacket(1, 0, body[:], r.Cipher)
				if err == nil {
					r.sendRaw(pkt)
				} else {
					fmt.Printf("[RobotVo] LOGIN SEND ERR: %v\n", err)
				}
			}
			return
		}
		return
	}

	if packetFlag == 0 && packetType == 53 && r.State == StateLogin && !r.SelectCharacSent {
		r.sendSelectCharacUnsafe("after type=53")
		return
	}

	if packetFlag == 0 && packetType == 300 && r.State == StateLogin {
		pkt, err := buildSendPacket(37, 19, r.setPos[:], r.Cipher)
		if err == nil {
			r.sendRaw(pkt)
		}

		r.setArea[0] = r.CurVillage
		r.setArea[1] = r.CurArea
		binary.LittleEndian.PutUint16(r.setArea[2:4], r.CurX)
		binary.LittleEndian.PutUint16(r.setArea[4:6], r.CurY)
		r.setArea[7] = 0x01
		binary.LittleEndian.PutUint16(r.setArea[8:10], uint16(r.CurVillage))
		binary.LittleEndian.PutUint16(r.setArea[10:12], uint16(dnfGateAreaForVillage(int(r.CurVillage))))
		pkt, err = buildSendPacket(38, 26, r.setArea[:], r.Cipher)
		if err == nil {
			r.sendRaw(pkt)
		}

		binary.LittleEndian.PutUint16(r.setPosStart[0:2], r.CurX)
		binary.LittleEndian.PutUint16(r.setPosStart[2:4], r.CurY)
		pkt, err = buildSendPacket(37, 27, r.setPosStart[:], r.Cipher)
		if err == nil {
			if r.sendRaw(pkt) {
				if r.IsRefishUser == 1 && r.DB != nil {
					_, _ = r.DB.Exec("update taiwan_cain_2nd.inventory set money=1000000 where charac_no in(select b.charac_no from d_taiwan.accounts a left join taiwan_cain.charac_info b on a.uid=b.m_id where a.uid=?);", r.UID)
				} else {
					r.PacketID = 29
					r.State = StateRun
					r.ConnCount = 0
					r.sendNATInfoUnsafe()
					r.sendPartyOptionUnsafe()
					if r.RunStartTime == 0 {
						r.RunStartTime = uint32(time.Now().Unix())
					}
					if r.Controller != nil {
						r.Controller.AddMessageDelay("MsgOnLineAsyncTaskVec", r, int(r.RunStartTime)+1)
					}
					r.runAsyncTasks()
				}
			}
		}
		return
	}
}

func (r *RobotVo) runAsyncTasks() {
	if r.AfterRunAsyncTaskVec == nil {
		return
	}
	tasks := r.AfterRunAsyncTaskVec
	r.AfterRunAsyncTaskVec = nil
	cost := uint32(0)
	title := ""
	for _, task := range tasks {
		switch task.Type {
		case AsyncDisjoint:
			cost = uint32(task.Cost)
		case AsyncPriStore:
			title = task.Title
		}
	}

	go r.executeAsyncTasks(tasks, cost, title)
}

func (r *RobotVo) executeAsyncTasks(tasks []AsyncTask, cost uint32, title string) {
	defer func() {
		if rec := recover(); rec != nil {
			fmt.Printf("[RobotVo] runAsyncTasks panic uid=%d err=%v\n", r.UID, rec)
		}
	}()
	for _, task := range tasks {
		switch task.Type {
		case AsyncDisjoint:
			r.OpenDisjointStore(cost)
		case AsyncPriStore:
			r.mu.Lock()
			r.PendingStoreTitle = title
			r.mu.Unlock()
			r.CreatePrivateStore()
			r.GetCompleteDisplay(0)
		}
	}
}

func (r *RobotVo) sendRaw(pkt []byte) bool {
	r.sendMu.Lock()
	defer r.sendMu.Unlock()

	if r.Conn == nil || len(pkt) == 0 {
		return false
	}
	for written := 0; written < len(pkt); {
		_ = r.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		n, err := r.Conn.Write(pkt[written:])
		if err != nil {
			fmt.Printf("[RobotVo] write failed uid=%d err=%v\n", r.UID, err)
			return false
		}
		if n == 0 {
			fmt.Printf("[RobotVo] write made no progress uid=%d\n", r.UID)
			return false
		}
		written += n
	}
	return true
}

func (r *RobotVo) sendSelectCharacUnsafe(_ string) bool {
	if r.State != StateLogin || r.SelectCharacSent {
		return false
	}
	r.IsTokenRight = true
	r.selectCharac[0] = byte(r.CID)
	pkt, err := buildSendPacket(4, 12, r.selectCharac[:], r.Cipher)
	if err != nil {
		return false
	}
	if !r.sendRaw(pkt) {
		return false
	}
	r.SelectCharacSent = true
	return true
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
	if r.partyRelayConn != nil || r.Conn == nil || !r.partyActiveUnsafe() {
		return
	}
	if addr, ok := r.Conn.LocalAddr().(*net.TCPAddr); ok {
		r.startPartyRelayUnsafe(addr.IP)
	}
}

func (r *RobotVo) startPartyRelayUnsafe(ip net.IP) {
	if r.partyRelayConn != nil || r.LoginIP == "" {
		return
	}
	host := r.LoginIP
	if ip != nil && ip.String() != "" {
		host = ip.String()
	}
	now := time.Now()
	if !r.partyRelayAt.IsZero() && now.Sub(r.partyRelayAt) < 5*time.Second {
		return
	}
	r.partyRelayAt = now
	relayAddr := net.JoinHostPort(host, strconv.Itoa(partyRelayPort))
	conn, err := net.DialTimeout("tcp", relayAddr, 3*time.Second)
	if err != nil {
		fmt.Printf("[PARTY_RELAY_CONNECT_ERROR] uid=%d addr=%s err=%v\n", r.UID, relayAddr, err)
		return
	}
	auth := buildPartyRelayPacket(0, r.UID, 0, nil)
	_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write(auth); err != nil {
		_ = conn.Close()
		fmt.Printf("[PARTY_RELAY_AUTH_ERROR] uid=%d err=%v\n", r.UID, err)
		return
	}
	r.partyRelayConn = conn
	go r.partyRelayLoop(conn, r.UID)
}

func (r *RobotVo) closePartyRelayUnsafe() {
	conn := r.partyRelayConn
	r.partyRelayConn = nil
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
	if r.partyRelayConn != conn {
		return false
	}
	r.partyRelayConn = nil
	return r.State != StateStop
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
					_ = conn.SetWriteDeadline(now.Add(3 * time.Second))
					if _, err := conn.Write(buildPartyRelayPacket(1, uid, uid, nil)); err != nil {
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
		_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
		if _, err := conn.Write(reply); err != nil {
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
	go r.partyUDPLoop(conn, r.UID)
	go r.partyUDPProbeLoop(conn)
	return true
}

func (r *RobotVo) closePartyUDPUnsafe() {
	if r.partyUDPConn != nil {
		_ = r.partyUDPConn.Close()
		r.partyUDPConn = nil
	}
}

func (r *RobotVo) partyUDPLoop(conn *net.UDPConn, uid uint32) {
	buf := make([]byte, 4096)
	for {
		r.mu.Lock()
		stopped := r.State == StateStop || r.partyUDPConn != conn
		if !stopped && r.partyActiveUnsafe() {
			r.startPartyRobotPeerNegotiationUnsafe()
		}
		r.mu.Unlock()
		if stopped {
			_ = conn.Close()
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
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

func (r *RobotVo) partyUDPProbeLoop(conn *net.UDPConn) {
	probeTicker := time.NewTicker(time.Second)
	followTicker := time.NewTicker(100 * time.Millisecond)
	defer probeTicker.Stop()
	defer followTicker.Stop()
	for {
		select {
		case <-probeTicker.C:
			r.mu.Lock()
			if r.State == StateStop || r.partyUDPConn != conn {
				r.mu.Unlock()
				return
			}
			if r.partyActiveUnsafe() {
				r.startPartyRobotPeerNegotiationUnsafe()
			}
			r.mu.Unlock()
		case <-followTicker.C:
			r.mu.Lock()
			if r.State == StateStop || r.partyUDPConn != conn {
				r.mu.Unlock()
				return
			}
			r.flushPartyDungeonFollowUnsafe(conn, time.Now())
			r.flushPartyDungeonSkillUnsafe(conn, time.Now())
			r.mu.Unlock()
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
	r.tracePartyUDPUnsafe("RECV", remote, senderSlot, peer.accID, len(payload))
	if len(payload) == 8 && payload[0] == 0x00 {
		return nil
	}
	return r.buildPartyTQOSRepliesUnsafe(payload, 1, peer)
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
	if !r.partySelfPeer.slotKnown || !peer.slotKnown {
		return nil
	}
	frames, ok := splitPartyTransportFrames(payload)
	if !ok {
		return nil
	}
	replies := make([][]byte, 0, len(frames)+1)
	for _, frame := range frames {
		if frame[0] == 0x00 {
			continue
		}
		if frame[7] != peer.slot {
			return nil
		}
		if frame[0] == 0x01 {
			sequence := binary.LittleEndian.Uint32(frame[1:5])
			replies = append(replies, buildPartyTQOSAck(r.partySelfPeer.slot, sequence))
		}
		if r.shouldFollowPartyPeerUnsafe(peer) {
			r.rememberPartyDungeonActivityUnsafe(frame, route, peer, time.Now())
			r.tracePartyDungeonFrameUnsafe(frame, route, peer)
			if route == 1 {
				r.queuePartyDungeonFollowUnsafe(frame, peer, time.Now())
			}
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
		if route == 1 && peer.slot < 4 && isPartyRobotAccount(peer.accID) && !r.partyRobotPeerReady[peer.slot] {
			r.partyRobotPeerReady[peer.slot] = true
			fmt.Printf("[PARTY_ROBOT_TQOS_READY] uid=%d peer=%d slot=%d state=%d\n", r.UID, peer.accID, peer.slot, request.state)
		}
		if peer.slot < 4 && route < 3 {
			r.partyTQOSCodecs[peer.slot][route] = request.codec
			r.partyTQOSCodecKnown[peer.slot][route] = true
		}
		nextState, hasNextState := nextPartyTQOSState(request.state)
		if hasNextState {
			sequence, ok := r.nextPartyTQOSSequenceUnsafe(peer.slot, route, nextState == 2)
			if !ok {
				continue
			}
			reply := buildPartyTQOSPacket(sequence, r.partySelfPeer.slot, request.flags, nextState, route, request.codec)
			replies = append(replies, reply)
		}
	}
	return replies
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

func (r *RobotVo) startPartyRobotPeerNegotiationUnsafe() {
	if r.partyUDPConn == nil || !r.partySelfPeer.slotKnown || !isPartyRobotAccount(r.partySelfPeer.accID) {
		return
	}
	for _, peer := range r.partyPeers {
		if !peer.slotKnown || peer.slot >= 4 || r.partyRobotPeerReady[peer.slot] || r.partyRobotProbeCount[peer.slot] >= 4 || !isPartyRobotAccount(peer.accID) {
			continue
		}
		if r.partySelfPeer.accID == peer.accID || peer.outerIP == nil || peer.port == 0 {
			continue
		}
		now := time.Now()
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
	if r.partyUDPConn == nil || !peer.slotKnown || peer.slot >= 4 || peer.outerIP == nil || peer.port == 0 {
		return false
	}
	sequence, ok := r.nextPartyTQOSSequenceUnsafe(peer.slot, 1, false)
	if !ok {
		return false
	}
	payload := buildPartyTQOSPacket(sequence, r.partySelfPeer.slot, 0, 3, 1, partyTQOSCodec{key: 0x7e})
	remote := &net.UDPAddr{IP: peer.outerIP, Port: int(peer.port)}
	if _, err := r.partyUDPConn.WriteToUDP(payload, remote); err != nil {
		fmt.Printf("[PARTY_ROBOT_PROBE_ERROR] uid=%d peer=%d attempt=%d remote=%s err=%v\n", r.UID, peer.accID, attempt, remote.String(), err)
		return false
	}
	fmt.Printf("[PARTY_ROBOT_PROBE] uid=%d peer=%d slot=%d attempt=%d sequence=%d remote=%s\n", r.UID, peer.accID, peer.slot, attempt, sequence, remote.String())
	return true
}

func isPartyRobotAccount(accID uint32) bool {
	return accID >= 17000000 && accID < 18000000
}

func (r *RobotVo) shouldFollowPartyPeerUnsafe(peer partyIPPeer) bool {
	return r.partySelfPeer.slotKnown && r.partySelfPeer.slot != 0 && peer.slotKnown && peer.slot == 0 && peer.uniqueID != 0
}

func (r *RobotVo) tracePartyDungeonFrameUnsafe(frame []byte, route byte, peer partyIPPeer) {
	now := time.Now()
	if now.Before(r.partyDungeonTraceAt) || len(frame) < 9 {
		return
	}
	r.partyDungeonTraceAt = now.Add(2 * time.Second)
	bodySize := binary.LittleEndian.Uint16(frame[5:7])
	follow := false
	if frame[0] == 0x02 {
		_, _, follow = rewritePartyDungeonBody(frame[9:], peer.uniqueID, r.partySelfPeer.uniqueID)
	}
	foundationlog.Robotf("[PARTY_DUNGEON_FRAME] uid=%d route=%d peer_slot=%d peer_unique_id=%d type=%d body=%d records=%s follow=%t\n", r.UID, route, peer.slot, peer.uniqueID, frame[0], bodySize, partyDungeonFrameRecords(frame), follow)
}

func (r *RobotVo) queuePartyDungeonFollowUnsafe(frame []byte, peer partyIPPeer, now time.Time) bool {
	if len(frame) < 9 || !peer.slotKnown || peer.slot >= 4 {
		return false
	}
	bodySize := int(binary.LittleEndian.Uint16(frame[5:7]))
	if len(frame) != 9+bodySize {
		return false
	}
	pending := partyDungeonFollowPending{
		due:      now.Add(r.partyDungeonFollowDelayUnsafe()),
		peerSlot: peer.slot,
		flags:    frame[8],
	}
	switch frame[0] {
	case 0x02:
		body, _, ok := rewritePartyDungeonBody(frame[9:], peer.uniqueID, r.partySelfPeer.uniqueID)
		if !ok {
			return false
		}
		pending.body = body
	case 0x01:
		records := rewritePartyDungeonRecords(frame[9:], peer.uniqueID, r.partySelfPeer.uniqueID)
		if len(records) == 0 {
			return false
		}
		pending.reliable = true
		pending.records = records
	default:
		return false
	}
	if len(r.partyDungeonFollow) >= 2048 {
		r.partyDungeonFollow = nil
		return false
	}
	r.partyDungeonFollow = append(r.partyDungeonFollow, pending)
	return true
}

func (r *RobotVo) partyDungeonFollowDelayUnsafe() time.Duration {
	return time.Duration(2000+int(r.UID%2001)) * time.Millisecond
}

func (r *RobotVo) rememberPartyDungeonActivityUnsafe(frame []byte, route byte, peer partyIPPeer, now time.Time) {
	if route != 1 || !r.shouldFollowPartyPeerUnsafe(peer) || len(frame) < 9 {
		return
	}
	if !partyDungeonFrameContainsCommand(frame, 0x0004) && !partyDungeonFrameContainsCommand(frame, 0x0027) && !partyDungeonFrameContainsCommand(frame, 0x0051) {
		return
	}
	r.partyDungeonLastAt = now
	r.partyDungeonFlags = frame[8]
	if r.partySkillNextAt.IsZero() {
		r.partySkillNextAt = now.Add(partySkillDelay(r.UID, now))
		foundationlog.Robotf("[PARTY_DUNGEON_SKILL_SCHEDULE] uid=%d due_in=%s type=%d records=%s\n", r.UID, r.partySkillNextAt.Sub(now), frame[0], partyDungeonFrameRecords(frame))
	}
}

func (r *RobotVo) flushPartyDungeonSkillUnsafe(conn *net.UDPConn, now time.Time) {
	if conn == nil || r.partyDungeonLastAt.IsZero() || now.Sub(r.partyDungeonLastAt) > 3*time.Second {
		if !r.partySkillNextAt.IsZero() || !r.partySkillRecoverAt.IsZero() {
			foundationlog.Robotf("[PARTY_DUNGEON_SKILL_EXPIRED] uid=%d idle=%s due_in=%s recover_in=%s\n", r.UID, now.Sub(r.partyDungeonLastAt), r.partySkillNextAt.Sub(now), r.partySkillRecoverAt.Sub(now))
		}
		r.partySkillNextAt = time.Time{}
		r.partySkillRecoverAt = time.Time{}
		return
	}
	if !r.partySkillRecoverAt.IsZero() && !now.Before(r.partySkillRecoverAt) {
		if r.sendPartySkillStateUnsafe(conn, now, 0, nil, "RECOVER") {
			r.partySkillRecoverAt = time.Time{}
		}
	}
	if r.partySkillNextAt.IsZero() || now.Before(r.partySkillNextAt) {
		return
	}
	r.partySkillNextAt = now.Add(partySkillDelay(r.UID, now))
	foundationlog.Robotf("[PARTY_DUNGEON_SKILL_DUE] uid=%d idle=%s\n", r.UID, now.Sub(r.partyDungeonLastAt))
	if !r.loadPartySkillProfileUnsafe() || len(r.partySkillCandidates) == 0 {
		return
	}
	peer := r.partyPeerForSlotUnsafe(0)
	if peer.uniqueID == 0 || peer.outerIP == nil || peer.port == 0 {
		return
	}
	candidate := r.nextPartySkillCandidateUnsafe(now)
	if r.sendPartySkillStateUnsafe(conn, now, candidate.state, candidate.stateData, "CAST") {
		r.partySkillRecoverAt = now.Add(partySkillRecoverDelay)
		foundationlog.Robotf("[PARTY_DUNGEON_SKILL] uid=%d job=%d skill=%d state=%d level=%d name=%s data=%x risk=%d learned=%t path=%s recover_in=%s\n", r.UID, r.partySkillJob, candidate.skillIndex, candidate.state, candidate.level, candidate.name, candidate.stateData, candidate.risk, candidate.learned, candidate.path, r.partySkillRecoverAt.Sub(now))
	}
}

func (r *RobotVo) nextPartySkillCandidateUnsafe(now time.Time) partySkillCandidate {
	if len(r.partySkillCandidates) == 0 {
		return partySkillCandidate{}
	}
	return r.partySkillCandidates[partySkillChoice(r.UID, now, len(r.partySkillCandidates))]
}

func (r *RobotVo) sendPartySkillStateUnsafe(conn *net.UDPConn, now time.Time, state byte, stateData []byte, reason string) bool {
	peer := r.partyPeerForSlotUnsafe(0)
	if peer.uniqueID == 0 || peer.outerIP == nil || peer.port == 0 {
		return false
	}
	body := buildPartySkillStateBody(r.partySelfPeer.uniqueID, state, stateData, partySkillToken(r.UID, now))
	sequence, ok := r.nextPartyTQOSSequenceUnsafe(peer.slot, 1, true)
	if !ok {
		return false
	}
	payload := buildPartyReliablePacket(sequence, r.partySelfPeer.slot, r.partyDungeonFlags, [][]byte{body})
	remote := &net.UDPAddr{IP: peer.outerIP, Port: int(peer.port)}
	if _, err := conn.WriteToUDP(payload, remote); err != nil {
		foundationlog.Robotf("[PARTY_DUNGEON_SKILL_%s_ERROR] uid=%d state=%d data=%x remote=%s err=%v\n", reason, r.UID, state, stateData, remote.String(), err)
		return false
	}
	foundationlog.Robotf("[PARTY_DUNGEON_SKILL_%s] uid=%d state=%d data=%x sequence=%d remote=%s\n", reason, r.UID, state, stateData, sequence, remote.String())
	return true
}

func (r *RobotVo) loadPartySkillProfileUnsafe() bool {
	if r.partySkillLoaded {
		return true
	}
	db := r.DB
	if db == nil {
		db = GetDBPool()
	}
	if db == nil || r.UID == 0 {
		foundationlog.Robotf("[PARTY_DUNGEON_SKILL_PROFILE_ERROR] uid=%d cid=%d db_ready=%t\n", r.UID, r.CID, db != nil)
		return false
	}
	var cid int
	var job int
	var raw []byte
	err := db.QueryRow("SELECT c.charac_no,c.job,UNCOMPRESS(s.skill_slot) FROM taiwan_cain.charac_info c JOIN taiwan_cain_2nd.skill s ON s.charac_no=c.charac_no WHERE c.m_id=? AND (?=0 OR c.charac_no=?) AND c.delete_flag=0 ORDER BY c.charac_no LIMIT 1", r.UID, r.CID, r.CID).Scan(&cid, &job, &raw)
	if err != nil {
		foundationlog.Robotf("[PARTY_DUNGEON_SKILL_PROFILE_ERROR] uid=%d cid=%d err=%v\n", r.UID, r.CID, err)
		return false
	}
	if r.CID == 0 {
		r.CID = cid
	}
	r.partySkillLoaded = true
	learned := parsePartyLearnedSkills(raw)
	whitelist, whitelistLoaded := loadPartySkillCatalogStatesForJob(job)
	pvfStates := shared.SkillStatesForJob(job)
	r.partySkillJob = job
	var stats partySkillCandidateStats
	r.partySkillCandidates, stats = partySkillCandidatesFromCatalog(job, learned, whitelist, pvfStates)
	foundationlog.Robotf("[PARTY_DUNGEON_SKILL_PROFILE] uid=%d cid=%d job=%d learned=%d whitelist_loaded=%t whitelist=%d pvf=%d pvf_matched=%d candidates=%d skipped_unlearned=%d skipped_missing_pvf=%d\n", r.UID, r.CID, job, len(learned), whitelistLoaded, len(whitelist), len(pvfStates), stats.PVFMatched, len(r.partySkillCandidates), stats.SkippedUnlearned, stats.SkippedMissingPVF)
	return true
}

func (r *RobotVo) resetPartySkillProfileUnsafe() {
	r.partySkillLoaded = false
	r.partySkillJob = 0
	r.partySkillCandidates = nil
}

func (r *RobotVo) flushPartyDungeonFollowUnsafe(conn *net.UDPConn, now time.Time) {
	for len(r.partyDungeonFollow) > 0 && !now.Before(r.partyDungeonFollow[0].due) {
		pending := r.partyDungeonFollow[0]
		r.partyDungeonFollow = r.partyDungeonFollow[1:]
		peer := r.partyPeerForSlotUnsafe(pending.peerSlot)
		if peer.uniqueID == 0 || peer.outerIP == nil || peer.port == 0 {
			continue
		}
		sequence, ok := r.nextPartyTQOSSequenceUnsafe(peer.slot, 1, pending.reliable)
		if !ok {
			continue
		}
		var payload []byte
		if pending.reliable {
			payload = buildPartyReliablePacket(sequence, r.partySelfPeer.slot, pending.flags, pending.records)
		} else {
			payload = buildPartyUnreliablePacket(sequence, r.partySelfPeer.slot, pending.flags, pending.body)
		}
		writePartyUDPReply(conn, payload, &net.UDPAddr{IP: peer.outerIP, Port: int(peer.port)}, r.UID)
	}
}

func (r *RobotVo) partyPeerForSlotUnsafe(slot byte) partyIPPeer {
	for _, peer := range r.partyPeers {
		if peer.slotKnown && peer.slot == slot {
			return peer
		}
	}
	return partyIPPeer{}
}

func partyDungeonFrameContainsCommand(frame []byte, target uint16) bool {
	if len(frame) < 12 {
		return false
	}
	if frame[0] == 0x02 {
		return binary.LittleEndian.Uint16(frame[10:12]) == target
	}
	if frame[0] != 0x01 {
		return false
	}
	body := frame[9:]
	for len(body) >= 2 {
		size := int(binary.LittleEndian.Uint16(body[:2]))
		body = body[2:]
		if size <= 0 || size > len(body) {
			return false
		}
		if size >= 3 && binary.LittleEndian.Uint16(body[1:3]) == target {
			return true
		}
		body = body[size:]
	}
	return false
}

func partyDungeonFrameRecords(frame []byte) string {
	if len(frame) < 9 {
		return "invalid"
	}
	if frame[0] == 0x02 {
		if len(frame) < 12 {
			return "short"
		}
		return fmt.Sprintf("0x%04x/%d", binary.LittleEndian.Uint16(frame[10:12]), len(frame)-9)
	}
	if frame[0] != 0x01 {
		return "control"
	}
	body := frame[9:]
	records := ""
	for count := 0; len(body) >= 2 && count < 8; count++ {
		size := int(binary.LittleEndian.Uint16(body[:2]))
		body = body[2:]
		if size <= 0 || size > len(body) {
			return records + "invalid"
		}
		command := uint16(0)
		if size >= 3 {
			command = binary.LittleEndian.Uint16(body[1:3])
		}
		if records != "" {
			records += ","
		}
		records += fmt.Sprintf("0x%04x/%d", command, size)
		body = body[size:]
	}
	if records == "" {
		return "empty"
	}
	return records
}

func (r *RobotVo) partyPeerForUDPUnsafe(remote *net.UDPAddr, senderSlot *byte) (partyIPPeer, bool) {
	if remote == nil || remote.IP == nil || remote.Port <= 0 || remote.Port > 0xffff {
		return partyIPPeer{}, false
	}
	for _, peer := range r.partyPeers {
		if peer.uniqueID == 0 || peer.outerIP == nil || peer.port == 0 {
			continue
		}
		if senderSlot != nil && (!peer.slotKnown || peer.slot != *senderSlot) {
			continue
		}
		if peer.outerIP.Equal(remote.IP) && peer.port == uint16(remote.Port) {
			return peer, true
		}
	}
	return partyIPPeer{}, false
}

func (r *RobotVo) partyPeerForAccountUnsafe(accID uint32) (partyIPPeer, bool) {
	if accID == 0 {
		return partyIPPeer{}, false
	}
	for _, peer := range r.partyPeers {
		if peer.accID == accID {
			return peer, true
		}
	}
	return partyIPPeer{}, false
}

const partyTownPositionMaxAge = 15 * time.Second

type townEntityPosition struct {
	uniqueID uint16
	x        uint16
	y        uint16
	moveType byte
	speed    uint16
	seenAt   time.Time
}

type townEntityArea struct {
	uniqueID uint16
	village  uint8
	area     uint8
	x        uint16
	y        uint16
}

func parseTownEntityPosition(data []byte) (townEntityPosition, bool) {
	if len(data) < 9 {
		return townEntityPosition{}, false
	}
	uniqueID := binary.LittleEndian.Uint16(data[:2])
	if uniqueID == 0 {
		return townEntityPosition{}, false
	}
	return townEntityPosition{
		uniqueID: uniqueID,
		x:        binary.LittleEndian.Uint16(data[2:4]),
		y:        binary.LittleEndian.Uint16(data[4:6]),
		moveType: data[6],
		speed:    binary.LittleEndian.Uint16(data[7:9]),
		seenAt:   time.Now(),
	}, true
}

func parseTownEntityArea(data []byte) (townEntityArea, bool) {
	if len(data) < 8 {
		return townEntityArea{}, false
	}
	uniqueID := binary.LittleEndian.Uint16(data[:2])
	if uniqueID == 0 {
		return townEntityArea{}, false
	}
	return townEntityArea{
		uniqueID: uniqueID,
		village:  data[2],
		area:     data[3],
		x:        binary.LittleEndian.Uint16(data[4:6]),
		y:        binary.LittleEndian.Uint16(data[6:8]),
	}, true
}

func (r *RobotVo) rememberTownEntityUnsafe(data []byte) (townEntityPosition, bool) {
	position, ok := parseTownEntityPosition(data)
	if !ok {
		return townEntityPosition{}, false
	}
	if r.townEntityPositions == nil {
		r.townEntityPositions = make(map[uint16]townEntityPosition)
	}
	r.townEntityPositions[position.uniqueID] = position
	if len(r.townEntityPositions) > 512 {
		cutoff := position.seenAt.Add(-partyTownPositionMaxAge)
		for uniqueID, cached := range r.townEntityPositions {
			if cached.seenAt.Before(cutoff) {
				delete(r.townEntityPositions, uniqueID)
			}
		}
	}
	return position, true
}

func (r *RobotVo) partyLeaderUniqueIDUnsafe() (uint16, bool) {
	if !r.partySelfPeer.slotKnown || r.partySelfPeer.slot == 0 {
		return 0, false
	}
	for _, peer := range r.partyPeers {
		if peer.slotKnown && peer.slot == 0 && peer.uniqueID != 0 {
			return peer.uniqueID, true
		}
	}
	return 0, false
}

func (r *RobotVo) followCachedPartyLeaderTownPositionUnsafe() bool {
	leaderID, ok := r.partyLeaderUniqueIDUnsafe()
	if !ok {
		return false
	}
	position, ok := r.townEntityPositions[leaderID]
	if !ok || position.seenAt.IsZero() || time.Since(position.seenAt) > partyTownPositionMaxAge {
		return false
	}
	return r.followPartyLeaderTownPositionUnsafe(position)
}

func (r *RobotVo) followPartyLeaderTownAreaUnsafe(area townEntityArea) bool {
	if r.State != StateRun {
		return false
	}
	leaderID, ok := r.partyLeaderUniqueIDUnsafe()
	if !ok || area.uniqueID != leaderID || (r.CurVillage == area.village && r.CurArea == area.area) {
		return false
	}
	r.setAreaFromLocked(area.village, area.area, area.x, area.y, uint16(r.CurVillage), uint16(r.CurArea))
	return r.CurVillage == area.village && r.CurArea == area.area
}

func (r *RobotVo) followPartyLeaderTownPositionUnsafe(position townEntityPosition) bool {
	if r.State != StateRun || position.uniqueID == 0 {
		return false
	}
	leaderID, ok := r.partyLeaderUniqueIDUnsafe()
	if !ok || position.uniqueID != leaderID || (r.CurX == position.x && r.CurY == position.y) {
		return false
	}
	moveType := position.moveType
	if moveType == 0 {
		moveType = 5
	}
	speed := position.speed
	if speed == 0 {
		speed = 100
	}
	return r.setPositionUnsafe(position.x, position.y, moveType, speed)
}

const partyPendingTimeout = 15 * time.Second

func (r *RobotVo) partyActiveUnsafe() bool {
	if r.partyPendingPeer != 0 && (r.partyPendingUntil.IsZero() || !time.Now().Before(r.partyPendingUntil)) {
		r.clearPartyPendingUnsafe()
	}
	for _, peer := range r.partyPeers {
		if peer.uniqueID != 0 {
			return true
		}
	}
	return r.partyPendingPeer != 0
}

func (r *RobotVo) setPartyPendingUnsafe(uniqueID uint16) {
	if uniqueID == 0 {
		r.clearPartyPendingUnsafe()
		return
	}
	r.partyPendingPeer = uniqueID
	r.partyPendingUntil = time.Now().Add(partyPendingTimeout)
}

func (r *RobotVo) clearPartyPendingUnsafe() {
	r.partyPendingPeer = 0
	r.partyPendingUntil = time.Time{}
}

func (r *RobotVo) rememberPartyPeersUnsafe(peers []partyIPPeer) {
	for _, peer := range peers {
		if peer.uniqueID == 0 {
			continue
		}
		known := false
		for i, existing := range r.partyPeers {
			if existing.uniqueID == peer.uniqueID {
				r.partyPeers[i] = mergePartyPeer(r.partyPeers[i], peer)
				known = true
				break
			}
		}
		if known {
			continue
		}
		for i, existing := range r.partyPeers {
			if existing.uniqueID == 0 {
				r.partyPeers[i] = peer
				known = true
				break
			}
		}
		if !known {
			copy(r.partyPeers[1:], r.partyPeers[:len(r.partyPeers)-1])
			r.partyPeers[0] = peer
		}
	}
}

func (r *RobotVo) setPartyPeersUnsafe(peers []partyIPPeer) {
	r.clearPartyPendingUnsafe()
	previous := r.partyPeers
	r.partyPeers = [4]partyIPPeer{}
	for i := range peers {
		for _, old := range previous {
			if old.uniqueID == peers[i].uniqueID {
				peers[i] = mergePartyPeer(old, peers[i])
				break
			}
		}
	}
	r.rememberPartyPeersUnsafe(peers)
	for slot := byte(0); slot < 4; slot++ {
		before := partyPeerUniqueIDForSlot(previous, slot)
		after := partyPeerUniqueIDForSlot(r.partyPeers, slot)
		if before != after && (before != 0 || after != 0) {
			r.resetPartyTQOSPeerUnsafe(slot)
		}
	}
	if !r.partyActiveUnsafe() {
		r.partySelfPeer = partyIPPeer{}
		r.resetPartyTQOSTransportUnsafe()
	}
}

func (r *RobotVo) removePartyPeerUnsafe(uniqueID uint16) {
	if uniqueID != 0 {
		r.clearPartyPendingUnsafe()
	}
	if uniqueID == 0 || uniqueID == r.partySelfPeer.uniqueID {
		r.clearPartyUnsafe()
		return
	}
	for i, peer := range r.partyPeers {
		if peer.uniqueID == uniqueID {
			r.partyPeers[i] = partyIPPeer{}
		}
	}
	if !r.partyActiveUnsafe() {
		r.clearPartyUnsafe()
	}
}

func (r *RobotVo) clearPartyUnsafe() {
	r.partySelfPeer = partyIPPeer{}
	r.partyPeers = [4]partyIPPeer{}
	r.clearPartyPendingUnsafe()
	r.townEntityPositions = make(map[uint16]townEntityPosition)
	r.partyDungeonTraceAt = time.Time{}
	r.closePartyRelayUnsafe()
	r.resetPartyTQOSTransportUnsafe()
}

func (r *RobotVo) resetPartyTQOSTransportUnsafe() {
	r.partyTQOSSeq = [4][3]uint32{}
	r.partyTQOSReliableSeq = [4][3]uint32{}
	r.partyTQOSCodecs = [4][3]partyTQOSCodec{}
	r.partyTQOSCodecKnown = [4][3]bool{}
	r.partyRobotProbeAt = [4]time.Time{}
	r.partyRobotProbeCount = [4]uint8{}
	r.partyRobotPeerReady = [4]bool{}
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
	r.partyTQOSCodecs[slot] = [3]partyTQOSCodec{}
	r.partyTQOSCodecKnown[slot] = [3]bool{}
	r.partyRobotProbeAt[slot] = time.Time{}
	r.partyRobotProbeCount[slot] = 0
	r.partyRobotPeerReady[slot] = false
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

func mergePartyPeer(old, next partyIPPeer) partyIPPeer {
	if next.accID == 0 {
		next.accID = old.accID
	}
	if !next.slotKnown && old.slotKnown {
		next.slot = old.slot
		next.slotKnown = true
	}
	if next.innerIP == nil {
		next.innerIP = old.innerIP
	}
	if next.outerIP == nil {
		next.outerIP = old.outerIP
	}
	if next.port == 0 {
		next.port = old.port
	}
	if next.natType == 0 {
		next.natType = old.natType
	}
	if next.mtu == 0 {
		next.mtu = old.mtu
	}
	return next
}

func (r *RobotVo) SendMsg(buf []byte) bool {
	return r.sendRaw(buf)
}

func (r *RobotVo) SendPublicMessage(msgType int, msg []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != StateRun || r.partyActiveUnsafe() {
		return
	}

	msgStr := string(msg)
	sendMsgType := byte(msgType)
	if sendMsgType == 0 {
		sendMsgType = 0x03
	}
	if basePos := findSubstring(msgStr, "ext("); basePos >= 0 {
		fPos := basePos + 4
		if endP := findSubstring(msgStr[fPos:], ")"); endP >= 0 {
			if t, err := strconv.Atoi(msgStr[fPos : fPos+endP]); err == nil {
				sendMsgType = byte(t)
			}
			msg = msg[:basePos]
		}
	}

	r.sendPublicMessagePacket(sendMsgType, 0x24, msg)
}
func (r *RobotVo) sendPublicMessagePacket(msgType, flag byte, msg []byte) {
	realSize := 1 + 2 + 4 + 4 + len(msg)
	alinSize := alignTo16(realSize)
	data := make([]byte, alinSize)
	data[0] = msgType
	data[1] = flag
	binary.LittleEndian.PutUint32(data[7:11], uint32(len(msg)))
	copy(data[11:], msg)
	pkt, err := buildSendPacket(17, uint16(r.PacketID), data, r.Cipher)
	r.PacketID++
	if err == nil {
		r.SendMsg(pkt)
	}
}

func (r *RobotVo) SendPrivateMessage(msgType int, msg []byte, charcName []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != StateRun {
		return
	}

	realSize := 1 + 2 + 4 + 4 + len(msg) + 4 + len(charcName) + 1
	alinSize := alignTo16(realSize)
	data := make([]byte, alinSize)
	data[0] = byte(msgType)
	binary.LittleEndian.PutUint32(data[7:11], uint32(len(msg)))
	copy(data[11:], msg)
	binary.LittleEndian.PutUint32(data[11+len(msg):15+len(msg)], uint32(len(charcName)))
	copy(data[15+len(msg):], charcName)
	pkt, err := buildSendPacket(17, uint16(r.PacketID), data, r.Cipher)
	r.PacketID++
	if err == nil {
		r.SendMsg(pkt)
	}
}

func (r *RobotVo) SetArea(village, area uint8, x, y uint16) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != StateRun || r.partyActiveUnsafe() {
		return
	}

	r.setAreaFromLocked(village, area, x, y, uint16(r.CurVillage), uint16(r.CurArea))
}

func (r *RobotVo) SetAreaFrom(village, area uint8, x, y uint16, fromVillage, fromArea uint16) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != StateRun || r.partyActiveUnsafe() {
		return
	}

	r.setAreaFromLocked(village, area, x, y, fromVillage, fromArea)
}

func (r *RobotVo) setAreaFromLocked(village, area uint8, x, y uint16, fromVillage, fromArea uint16) {
	areaChanged := r.CurVillage != village || r.CurArea != area
	setArea := r.setArea
	setArea[0] = village
	setArea[1] = area
	binary.LittleEndian.PutUint16(setArea[2:4], x)
	binary.LittleEndian.PutUint16(setArea[4:6], y)
	setArea[7] = 0x01
	binary.LittleEndian.PutUint16(setArea[8:10], fromVillage)
	binary.LittleEndian.PutUint16(setArea[10:12], fromArea)

	pkt, err := buildSendPacket(38, uint16(r.PacketID), setArea[:], r.Cipher)
	r.PacketID++
	if err == nil {
		if r.SendMsg(pkt) {
			if areaChanged {
				r.townEntityPositions = make(map[uint16]townEntityPosition)
			}
			r.CurVillage = village
			r.CurArea = area
			r.CurX = x
			r.CurY = y
		}
	}
}

func (r *RobotVo) SetPosition(x, y uint16, typ uint8, speed uint16) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != StateRun || r.partyActiveUnsafe() {
		return
	}
	r.setPositionUnsafe(x, y, typ, speed)
}

func (r *RobotVo) setPositionUnsafe(x, y uint16, typ uint8, speed uint16) bool {
	var setPos [8]byte
	setPos[0] = 0xDA
	setPos[1] = 0x01
	setPos[2] = 0xEA
	setPos[4] = 0x05
	setPos[5] = 0x64

	binary.LittleEndian.PutUint16(setPos[0:2], x)
	binary.LittleEndian.PutUint16(setPos[2:4], y)
	setPos[4] = typ
	binary.LittleEndian.PutUint16(setPos[5:7], speed)

	pkt, err := buildSendPacket(37, uint16(r.PacketID), setPos[:], r.Cipher)
	r.PacketID++
	if err != nil || !r.SendMsg(pkt) {
		return false
	}
	r.CurX = x
	r.CurY = y
	r.MoveType = typ
	return true
}

func (r *RobotVo) OpenDisjointStore(cost uint32) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != StateRun || r.partyActiveUnsafe() {
		return false
	}

	var openDisjoint [16]byte
	openDisjoint[0] = 0x01
	openDisjoint[4] = 0x01
	binary.LittleEndian.PutUint32(openDisjoint[5:9], cost)
	binary.LittleEndian.PutUint16(openDisjoint[9:11], r.CurX)
	binary.LittleEndian.PutUint16(openDisjoint[11:13], r.CurY)

	pkt, err := buildSendPacket(238, uint16(r.PacketID), openDisjoint[:], r.Cipher)
	r.PacketID++
	if err != nil || !r.SendMsg(pkt) {
		return false
	}
	r.RobotTyp = 3
	r.DisjointCreateSent = true
	r.DisjointDirectAck = false
	r.DisjointActive = false
	r.LastDisjointError = 0
	fmt.Printf("[DISJOINT_238_SENT] uid=%d cost=%d x=%d y=%d\n", r.UID, cost, r.CurX, r.CurY)
	return true
}

func (r *RobotVo) CreatePrivateStore() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != StateRun || r.partyActiveUnsafe() {
		return
	}
	r.StoreCreateRejected = false

	var data [16]byte
	data[6] = 0xFF
	data[7] = 0xFF
	data[0] = r.CurVillage
	data[1] = r.CurArea
	binary.LittleEndian.PutUint16(data[2:4], r.CurX)
	binary.LittleEndian.PutUint16(data[4:6], r.CurY)
	pkt, err := buildSendPacket(88, uint16(r.PacketID), data[:], r.Cipher)
	r.PacketID++
	if err == nil {
		r.SendMsg(pkt)
	}
	r.RobotTyp = 2
}

func (r *RobotVo) CompleteDisplay(title string, storeInfo []StoreInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.completeDisplay(title, storeInfo)
}

func (r *RobotVo) completeDisplay(title string, storeInfo []StoreInfo) {
	if r.State != StateRun || r.partyActiveUnsafe() {
		return
	}
	if r.StoreDisplaySent {
		return
	}

	realSize := 4 + len(title) + 1 + len(storeInfo)*13 + 2
	alinSize := alignTo(realSize, 8)
	data := make([]byte, alinSize)

	binary.LittleEndian.PutUint32(data[0:4], uint32(len(title)))
	copy(data[4:], []byte(title))
	data[4+len(title)] = byte(len(storeInfo))

	pBuf := data[4+len(title)+1:]
	for i, si := range storeInfo {
		off := i * 13
		binary.LittleEndian.PutUint16(pBuf[off+0:off+2], uint16(si.Index))
		binary.LittleEndian.PutUint32(pBuf[off+2:off+6], uint32(si.Price))
		pBuf[off+6] = byte(si.BoxType)
		binary.LittleEndian.PutUint16(pBuf[off+7:off+9], uint16(si.BoxIndex))
		binary.LittleEndian.PutUint32(pBuf[off+9:off+13], uint32(si.Count))
	}
	endOff := 4 + len(title) + 1 + len(storeInfo)*13
	pBuf = data[endOff:]
	if len(pBuf) >= 2 {
		pBuf[0] = 0xFF
		pBuf[1] = 0xFF
	}

	pkt, err := buildSendPacket(90, uint16(r.PacketID), data, r.Cipher)
	r.PacketID++
	if err == nil {
		r.SendMsg(pkt)
		r.StoreDisplaySent = true
		r.StoreDisplayAck = false
	}
}

func (r *RobotVo) CheckUserState() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State == StateStop {
		return false
	}
	if r.DisconReason != NoDisconnect {
		if r.Conn != nil {
			r.Conn.Close()
			r.Conn = nil
		}
		r.closePartyUDPUnsafe()
		r.closePartyRelayUnsafe()
		r.State = StateStop
		return false
	}
	if r.State != StateRun {
		if r.Conn != nil {
			r.Conn.Close()
			r.Conn = nil
		}
		r.closePartyUDPUnsafe()
		r.closePartyRelayUnsafe()
		r.State = StateStop
		return false
	}
	partyActive := r.partyActiveUnsafe()
	if partyActive {
		r.ensurePartyRelayUnsafe()
		r.startPartyRobotPeerNegotiationUnsafe()
	} else if r.partyRelayConn != nil {
		r.closePartyRelayUnsafe()
	}
	return true
}

func (r *RobotVo) RefishConnect() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State == StateStop {
		if r.Conn != nil {
			r.Conn.Close()
			r.Conn = nil
		}
		r.closePartyUDPUnsafe()
		r.closePartyRelayUnsafe()
		if r.Controller != nil {
			r.Controller.Delete(r.UID)
		}
		return false
	}

	if r.State == StateRun || r.State == StateLogin || r.State == StateInit {
		r.recvBuffer = nil
		r.recvSize = 0

		newVo := NewRobotVo(r.DB)
		newVo.Controller = r.Controller
		newVo.Load(r.LoginInfo)
		newVo.RunStartTime = r.RunStartTime
		newVo.ConnCount = r.ConnCount + 1
		newVo.AfterRunAsyncTaskVec = r.AfterRunAsyncTaskVec
		newVo.PendingStoreTitle = r.PendingStoreTitle

		if r.Conn != nil {
			r.Conn.Close()
			r.Conn = nil
		}
		r.closePartyUDPUnsafe()
		r.closePartyRelayUnsafe()
		r.State = StateStop

		if newVo.ConnCount < newVo.MaxReConn {
			if r.Controller != nil {
				r.Controller.Delete(r.UID)
			}
			delaySec := int(newVo.ReDelay)
			if delaySec <= 0 {
				delaySec = 5
			} else {
				delaySec = int((time.Duration(newVo.ReDelay)*time.Millisecond + time.Second - 1) / time.Second)
			}
			if r.Controller != nil {
				r.Controller.AddMessageDelay("MsgOnLine", newVo, delaySec)
			}
		}
		return true
	}

	r.State = StateStop
	if r.Conn != nil {
		r.Conn.Close()
		r.Conn = nil
	}
	r.closePartyUDPUnsafe()
	r.closePartyRelayUnsafe()
	return true
}

func (r *RobotVo) getShopVo(functionType int) map[int]ShopVo {
	result := make(map[int]ShopVo)
	if r.DB == nil {
		return result
	}

	rows, err := sqlpkg.Select(r.DB, "select Trade_item,price,item_number from d_starsky.Robot_stall where function_type=? and state=1 and (UID=? or UID=0) order by UID", functionType, r.UID)
	if err != nil {
		fmt.Printf("getShopVo query error: %v\n", err)
		return result
	}

	for _, row := range rows {
		if len(row) >= 3 && row[0] != "" && row[1] != "" && row[2] != "" {
			tradeItem, _ := strconv.Atoi(row[0])
			price, _ := strconv.Atoi(row[1])
			itemNumber, _ := strconv.Atoi(row[2])
			if price > 0 {
				result[tradeItem] = ShopVo{TradeItem: tradeItem, Price: price, ItemNumber: itemNumber}
			}
		}
	}
	return result
}

func (r *RobotVo) GetCompleteDisplay(flag int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != StateRun || r.partyActiveUnsafe() {
		return false
	}
	if !r.StoreCreated {
		return false
	}

	r.IsWaitingItemList = true
	var data [8]byte
	data[0] = byte(flag)
	pkt, err := buildSendPacket(20, uint16(r.PacketID), data[:], r.Cipher)
	r.PacketID++
	if err == nil {
		r.SendMsg(pkt)
	}
	return err == nil
}

func (r *RobotVo) GetDbDataAndCompleteDisplay() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.partyActiveUnsafe() {
		return false
	}
	if !r.StoreCreated {
		return false
	}
	return r.getDbDataAndCompleteDisplayUnsafe()
}

func (r *RobotVo) getDbDataAndCompleteDisplayUnsafe() bool {
	if r.State != StateRun {
		return false
	}

	if len(r.InfanMap) == 0 || r.DB == nil {
		return false
	}

	rows, err := sqlpkg.Select(r.DB, "select Trade_item,price,item_number from d_starsky.Robot_stall where function_type=2 and state=1 and (UID=? or UID=0) order by UID", r.UID)
	if err != nil {
		return false
	}

	var storeInfo []StoreInfo
	for _, row := range rows {
		if len(row) >= 3 && row[0] != "" && row[1] != "" && row[2] != "" {
			tradeItem, _ := strconv.Atoi(row[0])
			price, _ := strconv.Atoi(row[1])
			itemNumber, _ := strconv.Atoi(row[2])
			if price > 0 {
				if tx, ok := r.InfanMap[tradeItem]; ok {
					storeInfo = append(storeInfo, StoreInfo{
						Index:    len(storeInfo),
						BoxIndex: int(tx.ItemPos),
						Price:    price,
						Count:    itemNumber,
					})
				}
			}
		}
	}

	if len(storeInfo) > 0 {
		title := r.PendingStoreTitle
		if title == "" {
			title = "store"
		}

		cfgRows, _ := sqlpkg.Select(r.DB, "select cfg_content from d_starsky.Robot_stall_config where cfg_type=3 and function_type=2 and state=1 and (UID=? or UID=0) order by UID", r.UID)
		if len(cfgRows) > 0 && len(cfgRows[0]) > 0 && r.PendingStoreTitle == "" {
			title = cfgRows[0][0]
		}

		r.completeDisplay(title, storeInfo)
	}

	return true
}

func (r *RobotVo) CompleteDisplayFromStallFallback() bool {
	r.mu.Lock()
	if r.State != StateRun || !r.StoreCreated || r.StoreDisplayAck || r.DB == nil {
		r.mu.Unlock()
		return false
	}
	uid := r.UID
	title := r.PendingStoreTitle
	r.mu.Unlock()

	rows, err := sqlpkg.Select(r.DB, "select Trade_item,price,item_number from d_starsky.Robot_stall where function_type=2 and state=1 and (UID=? or UID=0) order by UID,id limit 24", uid)
	if err != nil || len(rows) == 0 {
		return false
	}

	type invPos struct {
		BoxType      int
		RawBoxIndex  int
		GameBoxIndex int
		Count        int
	}
	inventory := make(map[int]invPos)
	var invRaw []byte
	err = r.DB.QueryRow("SELECT UNCOMPRESS(i.inventory) FROM taiwan_cain.charac_info c JOIN taiwan_cain_2nd.inventory i ON i.charac_no=c.charac_no WHERE c.m_id=? ORDER BY c.charac_no LIMIT 1", uid).Scan(&invRaw)
	if err == nil {
		for rawIndex := 0; rawIndex+1 < 249 && (rawIndex+1)*61 <= len(invRaw); rawIndex++ {
			slot := invRaw[rawIndex*61 : (rawIndex+1)*61]
			boxType := int(binary.BigEndian.Uint16(slot[0:2]))
			itemID := int(binary.LittleEndian.Uint32(slot[2:6]))
			count := int(binary.LittleEndian.Uint32(slot[7:11]))
			if boxType > 0 && itemID > 0 {
				gameIndex := rawIndex - 2
				if gameIndex <= 0 {
					gameIndex = rawIndex
				}
				pos := invPos{BoxType: boxType, RawBoxIndex: rawIndex, GameBoxIndex: gameIndex, Count: count}
				if old, ok := inventory[itemID]; !ok || (!isStoreStackableType(old.BoxType) && isStoreStackableType(boxType)) {
					inventory[itemID] = pos
				}
			}
		}
	}

	type storeRow struct {
		Price int
		Count int
		Pos   invPos
	}
	storeRows := make([]storeRow, 0, len(rows))
	for _, row := range rows {
		if len(row) < 3 || row[1] == "" || row[2] == "" {
			continue
		}
		tradeItem, _ := strconv.Atoi(row[0])
		price, _ := strconv.Atoi(row[1])
		itemNumber, _ := strconv.Atoi(row[2])
		if price <= 0 || itemNumber <= 0 {
			continue
		}
		pos, ok := inventory[tradeItem]
		if !ok {
			continue
		}
		if !isStoreStackableType(pos.BoxType) {
			continue
		}
		if pos.Count > 0 && itemNumber > pos.Count {
			itemNumber = pos.Count
		}
		storeRows = append(storeRows, storeRow{Price: price, Count: itemNumber, Pos: pos})
	}
	if len(storeRows) == 0 {
		return false
	}
	if title == "" {
		title = "store"
	}

	r.mu.Lock()
	if r.State != StateRun || !r.StoreCreated || r.StoreDisplayAck || r.UID != uid || r.DB == nil {
		r.mu.Unlock()
		return r.StoreDisplayAck
	}
	r.StoreDisplaySent = false
	r.StoreDisplayAck = false
	r.StoreDisplayRejected = false
	r.mu.Unlock()

	storeInfo := make([]StoreInfo, 0, len(storeRows))
	for i, sr := range storeRows {
		count := sr.Count
		if count <= 0 {
			count = 1
		}
		storeInfo = append(storeInfo, StoreInfo{Index: i, BoxType: 0, BoxIndex: sr.Pos.GameBoxIndex, Price: sr.Price, Count: count})
	}

	r.CompleteDisplay(title, storeInfo)
	time.Sleep(1400 * time.Millisecond)
	return r.Snapshot().StoreDisplayAck
}

func (r *RobotVo) Push20yMoneyAndInNull(typ int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.State = StateStop
	r.TradeMoney = 0
	r.LastTradeState = false
	r.LastTradeID = 0
	r.ConnCount++
	for i := 0; i < 24; i++ {
		r.TransactionArr[i] = nil
	}
	if r.Conn != nil {
		r.Conn.Close()
		r.Conn = nil
	}
	r.closePartyUDPUnsafe()
	r.closePartyRelayUnsafe()
	if r.Controller != nil {
		r.Controller.Delete(r.UID)
	}
	if r.ConnCount < r.MaxReConn {
		r.CID = typ
		r.IsRefishUser = typ
		if r.Controller != nil {
			r.Controller.AddMessage("MsgOnLine", r)
		}
	}
	return true
}

func buildSendPacket(sendType, sendIndex uint16, rawData []byte, cipher *crypt.DNFCipher) ([]byte, error) {
	outSize := 13 + len(rawData)
	outBuf := make([]byte, outSize)
	tmp := make([]byte, outSize)

	binary.LittleEndian.PutUint16(tmp[11:13], sendIndex)
	copy(tmp[13:], rawData)

	crc := cipher.CRC32(0, tmp[11:])
	hashBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(hashBytes, crc)
	hashBytes[0] ^= hashBytes[2] ^ hashBytes[1] ^ hashBytes[3] ^ 0x18
	hashVal := binary.LittleEndian.Uint32(hashBytes)

	outBuf[0] = 1
	binary.LittleEndian.PutUint16(outBuf[1:3], sendType)
	binary.LittleEndian.PutUint32(outBuf[3:7], uint32(outSize))
	binary.LittleEndian.PutUint32(outBuf[7:11], hashVal)
	binary.LittleEndian.PutUint16(outBuf[11:13], sendIndex)

	copy(tmp[:13], outBuf[:13])

	if len(rawData) > 0 {
		encrypted, err := cipher.Encrypt(sendType, rawData)
		if err != nil {
			return nil, err
		}
		copy(outBuf[13:], encrypted)
	}

	return outBuf, nil
}

const (
	peerRequestParty byte = iota
	peerRequestTrade
	gameEtcOptionSize = 72
	partyRejectOption = 6
)

func buildPeerResponse(request []byte) ([]byte, byte, bool) {
	if len(request) < 7 {
		return nil, 0, false
	}
	typ := request[2]
	if typ != peerRequestParty && typ != peerRequestTrade {
		return nil, typ, false
	}
	response := make([]byte, 8)
	copy(response, request[:7])
	return response, typ, true
}

func partyAcceptGameOptions(packet []byte) ([]byte, bool) {
	if len(packet) < 4 {
		return nil, false
	}
	size := int(binary.LittleEndian.Uint32(packet[0:4]))
	if size < gameEtcOptionSize || size > len(packet)-4 {
		return nil, false
	}
	options := make([]byte, gameEtcOptionSize)
	copy(options, packet[4:4+gameEtcOptionSize])
	binary.LittleEndian.PutUint16(options[partyRejectOption*2:], 0)
	return options, true
}

func defaultPartyAcceptGameOptions() []byte {
	options := make([]byte, gameEtcOptionSize)
	for i := 0; i < gameEtcOptionSize/2; i++ {
		binary.LittleEndian.PutUint16(options[i*2:], 0x7fff)
	}
	binary.LittleEndian.PutUint16(options[1*2:], 1)
	binary.LittleEndian.PutUint16(options[partyRejectOption*2:], 0)
	return options
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

type partyIPPeer struct {
	uniqueID  uint16
	accID     uint32
	slot      byte
	slotKnown bool
	innerIP   net.IP
	outerIP   net.IP
	port      uint16
	natType   byte
	mtu       uint32
}

type partyDungeonFollowPending struct {
	due      time.Time
	peerSlot byte
	flags    byte
	reliable bool
	body     []byte
	records  [][]byte
}

type partySkillCandidate struct {
	skillIndex byte
	state      byte
	level      int
	name       string
	stateData  []byte
	risk       int
	path       string
	learned    bool
}

type partySkillCatalogConfig struct {
	MaxSkillLevel int                      `json:"max_skill_level"`
	Skills        []partySkillCatalogEntry `json:"skills"`
}

type partySkillCatalogEntry struct {
	Disabled   bool   `json:"disabled,omitempty"`
	Job        int    `json:"job"`
	SkillIndex int    `json:"skill_index"`
	State      int    `json:"state"`
	Level      int    `json:"level"`
	Name       string `json:"name,omitempty"`
	ScriptPath string `json:"script_path,omitempty"`
	StateData  []int  `json:"state_data,omitempty"`
	Risk       int    `json:"risk,omitempty"`
}

type partySkillCandidateStats struct {
	PVFMatched        int
	SkippedUnlearned  int
	SkippedMissingPVF int
}

func partySkillCandidatesFromCatalog(job int, learned map[byte]byte, whitelist []shared.SkillState, pvfStates []shared.SkillState) ([]partySkillCandidate, partySkillCandidateStats) {
	pvfIndex := make(map[[3]int]bool, len(pvfStates))
	for _, entry := range pvfStates {
		if entry.Job != job || entry.SkillIndex <= 0 || entry.SkillIndex > 255 || entry.State < 0 || entry.State > 255 {
			continue
		}
		pvfIndex[[3]int{entry.Job, entry.SkillIndex, entry.State}] = true
	}
	candidates := make([]partySkillCandidate, 0, len(whitelist))
	stats := partySkillCandidateStats{}
	for _, entry := range whitelist {
		if entry.Job != job || entry.SkillIndex <= 0 || entry.SkillIndex > 255 || entry.State < 0 || entry.State > 255 {
			continue
		}
		if !pvfIndex[[3]int{entry.Job, entry.SkillIndex, entry.State}] {
			stats.SkippedMissingPVF++
			continue
		}
		stats.PVFMatched++
		known := learned[byte(entry.SkillIndex)] != 0
		candidates = append(candidates, partySkillCandidate{
			skillIndex: byte(entry.SkillIndex),
			state:      byte(entry.State),
			level:      entry.Level,
			name:       entry.Name,
			stateData:  append([]byte(nil), entry.StateData...),
			risk:       entry.Risk,
			path:       entry.ScriptPath,
			learned:    known,
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].risk != candidates[j].risk {
			return candidates[i].risk < candidates[j].risk
		}
		if candidates[i].skillIndex != candidates[j].skillIndex {
			return candidates[i].skillIndex < candidates[j].skillIndex
		}
		return candidates[i].state < candidates[j].state
	})
	return candidates, stats
}

func loadPartySkillCatalogStatesForJob(job int) ([]shared.SkillState, bool) {
	path := os.Getenv("PARTY_SKILL_CATALOG_CONFIG")
	if path == "" {
		path = filepath.Join("/root/config", "party_skill_catalog.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var cfg partySkillCatalogConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		foundationlog.Robotf("[PARTY_DUNGEON_SKILL_CATALOG_ERROR] path=%s err=%v\n", path, err)
		return nil, true
	}
	maxLevel := cfg.MaxSkillLevel
	if maxLevel <= 0 {
		maxLevel = 70
	}
	states := make([]shared.SkillState, 0)
	for _, entry := range cfg.Skills {
		if entry.Disabled || entry.Job != job || entry.Level <= 0 || entry.Level > maxLevel || entry.SkillIndex <= 0 || entry.SkillIndex > 255 || entry.State < 0 || entry.State > 255 {
			continue
		}
		stateData, ok := partySkillStateDataFromInts(entry.StateData)
		if !ok {
			foundationlog.Robotf("[PARTY_DUNGEON_SKILL_CATALOG_SKIP] job=%d skill=%d state=%d reason=bad_state_data\n", entry.Job, entry.SkillIndex, entry.State)
			continue
		}
		states = append(states, shared.SkillState{
			Job:          entry.Job,
			SkillIndex:   entry.SkillIndex,
			State:        entry.State,
			Level:        entry.Level,
			Name:         entry.Name,
			ScriptPath:   entry.ScriptPath,
			StateData:    stateData,
			Verified:     true,
			Experimental: true,
			Risk:         entry.Risk,
		})
	}
	return states, true
}

func partySkillStateDataFromInts(values []int) ([]byte, bool) {
	if len(values) > 3 {
		return nil, false
	}
	data := make([]byte, 0, len(values)*3)
	for _, value := range values {
		if value < 0 || value > 0xffffff {
			return nil, false
		}
		data = append(data, byte(value), byte(value>>8), byte(value>>16))
	}
	return data, true
}

func parsePartyIPInfoMembers(packet []byte) ([]partyIPPeer, bool) {
	if len(packet) < 1 {
		return nil, false
	}
	count := int(packet[0])
	if count > 4 {
		return nil, false
	}
	const entrySize = 22
	if len(packet) < 1+count*entrySize {
		return nil, false
	}
	peers := make([]partyIPPeer, 0, count)
	offset := 1
	for i := 0; i < count; i++ {
		id := binary.LittleEndian.Uint16(packet[offset : offset+2])
		innerIP := net.IPv4(packet[offset+2], packet[offset+3], packet[offset+4], packet[offset+5])
		outerIP := net.IPv4(packet[offset+6], packet[offset+7], packet[offset+8], packet[offset+9])
		port := binary.BigEndian.Uint16(packet[offset+10 : offset+12])
		accID := binary.LittleEndian.Uint32(packet[offset+12 : offset+16])
		natType := packet[offset+16]
		mtu := binary.LittleEndian.Uint32(packet[offset+17 : offset+21])
		if id != 0 {
			peers = append(peers, partyIPPeer{
				uniqueID:  id,
				accID:     accID,
				slot:      byte(i),
				slotKnown: true,
				innerIP:   innerIP,
				outerIP:   outerIP,
				port:      port,
				natType:   natType,
				mtu:       mtu,
			})
		}
		offset += entrySize
	}
	return peers, true
}

func parsePartyIPInfo(packet []byte, selfAccID uint32) []partyIPPeer {
	_, peers, ok := parsePartyIPInfoSnapshot(packet, selfAccID)
	if !ok {
		return nil
	}
	return peers
}

func parsePartySelfIPInfo(packet []byte, selfAccID uint32) partyIPPeer {
	self, _, _ := parsePartyIPInfoSnapshot(packet, selfAccID)
	return self

}

func parsePartyIPInfoSnapshot(packet []byte, selfAccID uint32) (partyIPPeer, []partyIPPeer, bool) {
	members, ok := parsePartyIPInfoMembers(packet)
	if !ok {
		return partyIPPeer{}, nil, false
	}
	peers := make([]partyIPPeer, 0, len(members))
	self := partyIPPeer{}
	for _, peer := range members {
		if selfAccID != 0 && peer.accID == selfAccID {
			self = peer
			continue
		}
		peers = append(peers, peer)
	}
	return self, peers, true
}

func tracePartyIPInfo(uid uint32, self partyIPPeer, peers []partyIPPeer) {
	peerText := ""
	for _, peer := range peers {
		if peerText != "" {
			peerText += ","
		}
		peerText += fmt.Sprintf("slot%d:acc%d:uid%d:port%d", peer.slot, peer.accID, peer.uniqueID, peer.port)
	}
	fmt.Printf("[PARTY_IPINFO] uid=%d self_slot=%d self_acc=%d self_unique=%d self_port=%d peers=%s\n",
		uid, self.slot, self.accID, self.uniqueID, self.port, peerText)
}

func inflatePartyInfo(data []byte) ([]byte, error) {
	zr, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

func partyInfoClearsParty(data []byte) bool {
	if len(data) < 8 || data[0] != 1 || (data[4] != 2 && data[4] != 3) {
		return false
	}
	return data[5] == 0xff && data[6] == 0xff && data[7] == 0xff
}

const partyTQOSBodySize = 10
const partyDungeonStateCommand = 0x0004
const partyDungeonEnvelopeCommand = 0x0051
const partyDungeonEnvelopeMinBodySize = 26
const partyDungeonEnvelopePayloadOffset = 22

const partySkillStateBodyBaseSize = 31
const partySkillRecoverDelay = 900 * time.Millisecond

var partyDungeonEnvelopeChecksumOffsets = [...]int{10, 18}

func parsePartyLearnedSkills(raw []byte) map[byte]byte {
	learned := make(map[byte]byte)
	for i := 0; i+1 < len(raw); i += 2 {
		if raw[i] != 0 && raw[i+1] != 0 {
			learned[raw[i]] = raw[i+1]
		}
	}
	return learned
}

func buildPartySkillStateBody(uniqueID uint16, state byte, stateData []byte, token uint16) []byte {
	body := make([]byte, partySkillStateBodyBaseSize+len(stateData))
	body[0] = 0x02
	binary.LittleEndian.PutUint16(body[1:3], partyDungeonEnvelopeCommand)
	body[7] = 0x02
	body[8] = 0x05
	body[14] = 0x00
	body[15] = 0x02
	body[16] = 0x05
	payload := body[partyDungeonEnvelopePayloadOffset:]
	payload[0] = 0x11
	payload[1] = 0x01
	binary.LittleEndian.PutUint16(payload[2:4], uniqueID)
	payload[4] = state
	binary.LittleEndian.PutUint16(payload[5:7], uint16(len(stateData)))
	copy(payload[7:], stateData)
	binary.LittleEndian.PutUint16(payload[7+len(stateData):9+len(stateData)], token)
	innerChecksum := partyPayloadChecksum(payload)
	for _, offset := range partyDungeonEnvelopeChecksumOffsets {
		copy(body[offset:offset+len(innerChecksum)], innerChecksum[:])
	}
	outerChecksum := partyPayloadChecksum(body[7:])
	body[3] = outerChecksum[0]
	body[4] = byte(token)
	body[5] = byte(token >> 8)
	body[6] = byte(uniqueID>>8) ^ state ^ byte(len(stateData))
	return body
}

func partySkillDelay(uid uint32, now time.Time) time.Duration {
	return time.Duration(4+partySkillChoice(uid, now, 6)) * time.Second
}

func partySkillChoice(uid uint32, now time.Time, count int) int {
	if count <= 1 {
		return 0
	}
	value := uint64(now.UnixNano()) ^ uint64(uid)*0x9e3779b97f4a7c15
	value ^= value >> 30
	value *= 0xbf58476d1ce4e5b9
	value ^= value >> 27
	return int(value % uint64(count))
}

func partySkillToken(uid uint32, now time.Time) uint16 {
	value := uint64(now.UnixNano()) ^ uint64(uid)<<32
	value ^= value >> 33
	value *= 0xff51afd7ed558ccd
	value ^= value >> 33
	return uint16(value)
}

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

func rewritePartyDungeonBody(body []byte, sourceUniqueID, targetUniqueID uint16) ([]byte, uint16, bool) {
	if len(body) < 7 || body[0] != 1 || sourceUniqueID == 0 || targetUniqueID == 0 || sourceUniqueID == targetUniqueID {
		return nil, 0, false
	}
	command := binary.LittleEndian.Uint16(body[1:3])
	followBody := append([]byte(nil), body...)
	var checksum [4]byte
	switch command {
	case partyDungeonEnvelopeCommand:
		if len(body) < partyDungeonEnvelopeMinBodySize {
			return nil, 0, false
		}
		checksum = partyPayloadChecksum(body[7:])
		if !bytes.Equal(body[3:7], checksum[:]) {
			return nil, 0, false
		}
		innerChecksum := partyPayloadChecksum(body[partyDungeonEnvelopePayloadOffset:])
		for _, offset := range partyDungeonEnvelopeChecksumOffsets {
			if !bytes.Equal(body[offset:offset+len(innerChecksum)], innerChecksum[:]) {
				return nil, 0, false
			}
		}
		if rewritePartyDungeonEnvelopeIdentity(followBody[partyDungeonEnvelopePayloadOffset:], sourceUniqueID, targetUniqueID) == 0 {
			return nil, 0, false
		}
		innerChecksum = partyPayloadChecksum(followBody[partyDungeonEnvelopePayloadOffset:])
		for _, offset := range partyDungeonEnvelopeChecksumOffsets {
			copy(followBody[offset:offset+len(innerChecksum)], innerChecksum[:])
		}
	case partyDungeonStateCommand:
		plain := followBody[7:]
		for i := range plain {
			plain[i] ^= 0x7e
		}
		checksum = partyPayloadChecksum(plain)
		if !bytes.Equal(body[3:7], checksum[:]) || rewritePartyDungeonStateIdentity(plain, sourceUniqueID, targetUniqueID) == 0 {
			return nil, 0, false
		}
		checksum = partyPayloadChecksum(plain)
		copy(followBody[3:7], checksum[:])
		for i := range plain {
			plain[i] ^= 0x7e
		}
	default:
		return nil, 0, false
	}
	if command == partyDungeonEnvelopeCommand {
		checksum = partyPayloadChecksum(followBody[7:])
		copy(followBody[3:7], checksum[:])
	}
	return followBody, command, true
}

func rewritePartyDungeonRecords(body []byte, sourceUniqueID, targetUniqueID uint16) [][]byte {
	records := make([][]byte, 0, 2)
	for len(body) >= 2 {
		size := int(binary.LittleEndian.Uint16(body[:2]))
		body = body[2:]
		if size <= 0 || size > len(body) {
			return nil
		}
		if record, _, ok := rewritePartyDungeonBody(body[:size], sourceUniqueID, targetUniqueID); ok {
			records = append(records, record)
		}
		body = body[size:]
	}
	if len(body) != 0 {
		return nil
	}
	return records
}

func rewritePartyDungeonEnvelopeIdentity(payload []byte, sourceUniqueID, targetUniqueID uint16) int {
	if len(payload) < 6 {
		return 0
	}
	replacements := 0
	for _, offset := range [...]int{2, 4} {
		if binary.LittleEndian.Uint16(payload[offset:]) != sourceUniqueID {
			continue
		}
		binary.LittleEndian.PutUint16(payload[offset:], targetUniqueID)
		replacements++
	}
	return replacements
}

func rewritePartyDungeonStateIdentity(payload []byte, sourceUniqueID, targetUniqueID uint16) int {
	const singleStateHeaderSize = 15
	if len(payload) < singleStateHeaderSize || payload[0] != 1 {
		return 0
	}
	replacements := 0
	for _, offset := range [...]int{3, 7} {
		if binary.LittleEndian.Uint16(payload[offset:]) != sourceUniqueID {
			continue
		}
		binary.LittleEndian.PutUint16(payload[offset:], targetUniqueID)
		replacements++
	}
	return replacements
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

func buildFinishLoadingPayload(inventoryChecksum, skillChecksum uint32) []byte {
	body := make([]byte, 8)
	binary.LittleEndian.PutUint32(body[:4], inventoryChecksum)
	binary.LittleEndian.PutUint32(body[4:], skillChecksum)
	return body
}

func parseRecvPacket(cipher *crypt.DNFCipher, raw []byte, isAnti bool) (dataType uint16, dataSize int, decrypted []byte, err error) {
	if len(raw) < 15 {
		return 0, 0, nil, fmt.Errorf("packet too short: %d bytes", len(raw))
	}

	dataType = binary.LittleEndian.Uint16(raw[1:3])

	encryptedData := raw[15:]
	if len(encryptedData) == 0 {
		return dataType, 0, nil, nil
	}

	if isAnti {
		dec := make([]byte, len(encryptedData))
		copy(dec, encryptedData)
		return dataType, len(dec), dec, nil
	}

	var decData []byte
	if dataType == 1 {
		decData, err = cipher.DecryptLogin(encryptedData)
	} else {
		decData, err = cipher.Decrypt(dataType, encryptedData)
	}

	if err != nil {
		return dataType, 0, nil, err
	}

	return dataType, len(decData), decData, nil
}

func alignTo16(size int) int {
	return alignTo(size, 16)
}

func alignTo(size, block int) int {
	return block * ((size / block) + boolToInt(size%block != 0))
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func dnfGateAreaForVillage(village int) int {
	if area, ok := dnfGateAreaByVillage[village]; ok {
		return area
	}
	return 1
}

var dnfGateAreaByVillage = map[int]int{
	1: 1, 2: 5, 3: 2, 4: 1, 5: 1, 6: 4, 8: 1, 9: 2, 10: 1, 11: 3,
	14: 3, 15: 0, 16: 0, 17: 0, 18: 0, 19: 0, 20: 0, 21: 7, 23: 0,
	24: 0, 25: 0, 26: 0,
}

func isStoreStackableType(itemType int) bool {
	switch itemType {
	case 2, 3, 4, 10:
		return true
	default:
		return false
	}
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
