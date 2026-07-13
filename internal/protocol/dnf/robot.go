package dnf

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"net"
	"robot/internal/foundation/lockhub"
	sqlpkg "robot/internal/foundation/sql"
	"robot/internal/protocol/dnf/crypt"
	"strconv"
	"time"
)

// ---- robot.go ----
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

	DisjointCreateSent bool
	DisjointDirectAck  bool
	DisjointActive     bool
	LastDisjointError  byte

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
	r.recvSize = 0
	r.Conn = nil
	r.LoginIP = info.IP
	r.LoginPort = info.Port
	r.LocalIP = "127.0.0.1"
}

func (r *RobotVo) CloseOut() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State == StateStop {
		return
	}
	if r.Conn != nil {
		r.Conn.Close()
		r.Conn = nil
	}
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

	if packetFlag == 0 && packetType == 561 && dInSize > 36 && pInBuf[22] == 0x11 && pInBuf[31] == 0x13 {
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

	if packetFlag == 0 && packetType == 7 && r.State == StateRun && !r.LastTradeState && r.LastTradeID == 0 {
		_, _, decData, err := parseRecvPacket(r.Cipher, pInBuf, isAnti)
		if err == nil && len(decData) >= 3 {
			uniqueID := binary.LittleEndian.Uint16(decData[0:2])
			typ := decData[2]
			r.LastTradeID = uniqueID
			if typ == 1 {
				var data [8]byte
				data[2] = 0x01
				data[3] = 0x88
				data[4] = 0xF4
				binary.LittleEndian.PutUint16(data[0:2], uniqueID)
				pkt, err := buildSendPacket(11, uint16(r.PacketID), data[:], r.Cipher)
				r.PacketID++
				if err == nil {
					if r.sendRaw(pkt) {
						r.LastTradeState = true
					} else {
						r.LastTradeState = false
					}
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
		binary.LittleEndian.PutUint16(r.setArea[10:12], uint16(r.CurArea))
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

func (r *RobotVo) SendMsg(buf []byte) bool {
	return r.sendRaw(buf)
}

func (r *RobotVo) SendPublicMessage(msgType int, msg []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != StateRun {
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

	if r.State != StateRun {
		return
	}

	r.setAreaFromLocked(village, area, x, y, uint16(r.CurVillage), uint16(r.CurArea))
}

func (r *RobotVo) SetAreaFrom(village, area uint8, x, y uint16, fromVillage, fromArea uint16) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != StateRun {
		return
	}

	r.setAreaFromLocked(village, area, x, y, fromVillage, fromArea)
}

func (r *RobotVo) setAreaFromLocked(village, area uint8, x, y uint16, fromVillage, fromArea uint16) {
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

	if r.State != StateRun {
		return
	}

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
	if err == nil {
		if r.SendMsg(pkt) {
			r.CurX = x
			r.CurY = y
			r.MoveType = typ
		}
	}
}

func (r *RobotVo) OpenDisjointStore(cost uint32) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State != StateRun {
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

	if r.State != StateRun {
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
	if r.State != StateRun {
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
		r.State = StateStop
		return false
	}
	if r.State != StateRun {
		if r.Conn != nil {
			r.Conn.Close()
			r.Conn = nil
		}
		r.State = StateStop
		return false
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

	if r.State != StateRun {
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
