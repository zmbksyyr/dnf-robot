package dnf

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

type MsgType int

const (
	MsgError     MsgType = 6000
	MsgList      MsgType = 6001
	MsgCreate    MsgType = 6002
	MsgRemove    MsgType = 6003
	MsgLogin     MsgType = 6004
	MsgLogout    MsgType = 6005
	MsgMove      MsgType = 6006
	MsgPublicMsg MsgType = 6007
	MsgOnLine    MsgType = 6008
)

type DnfTableTaskResult struct {
	Msg  string
	Code int
	UID  int
}

type RobotMsg struct {
	MsgFlag uint8
	Fd      int
	MsgType MsgType
	JSON    json.RawMessage
}

type MsgQueueData struct {
	Type         string
	Data         interface{}
	RunStartTime uint32
}

const (
	maxMessageQueueSize      = 5000
	maxMessageTimerQueueSize = 10000
)

type RobotStallConfig struct {
	CfgContent   string
	CfgType      int
	UID          int
	FunctionType int
}

type moveInternalData struct {
	ID       int
	Village  uint8
	Area     uint8
	X        uint16
	Y        uint16
	MoveType uint8
	Speed    uint16
}

type publicMsgInternalData struct {
	ID   int
	Msg  string
	Type int
}

type RobotDnfTask struct {
	messageQueue []MsgQueueData
	messageMutex sync.Mutex
	messageCond  *sync.Cond

	messageTimerQueue []MsgQueueData
	timerMutex        sync.Mutex

	robotVoMap   map[int]*RobotVo
	robotVoMutex sync.Mutex

	keyToHandle   map[string]func(task *RobotDnfTask, data interface{}) bool
	keyToHandleMu sync.Mutex

	done         chan struct{}
	shutdownOnce sync.Once
}

func NewRobotDnfTask() *RobotDnfTask {
	t := &RobotDnfTask{
		messageQueue:      make([]MsgQueueData, 0),
		messageTimerQueue: make([]MsgQueueData, 0),
		robotVoMap:        make(map[int]*RobotVo),
		keyToHandle:       make(map[string]func(task *RobotDnfTask, data interface{}) bool),
		done:              make(chan struct{}),
	}
	t.messageCond = sync.NewCond(&t.messageMutex)

	t.initKeyCall()

	go t.timerLoop()
	go t.dispatchLoop()

	return t
}

func (t *RobotDnfTask) initKeyCall() {
	t.keyToHandle["MsgOnLine"] = t.dnfMsgOnLine
	t.keyToHandle["MsgMove"] = t.dnfMsgMove
	t.keyToHandle["MsgLogout"] = t.msgLogout
	t.keyToHandle["MsgPublicMsg"] = t.msgPublicMsg
	t.keyToHandle["MsgOnLineAsyncTaskVec"] = t.msgOnLineAsyncTaskVec
}

func (t *RobotDnfTask) dispatchLoop() {
	for {
		select {
		case <-t.done:
			return
		default:
		}

		t.messageMutex.Lock()
		for len(t.messageQueue) == 0 {
			select {
			case <-t.done:
				t.messageMutex.Unlock()
				return
			default:
			}
			t.messageCond.Wait()
			select {
			case <-t.done:
				t.messageMutex.Unlock()
				return
			default:
			}
		}

		msg := t.messageQueue[0]
		t.messageQueue = t.messageQueue[1:]
		t.messageMutex.Unlock()

		t.handleMessage(msg)
	}
}

func (t *RobotDnfTask) handleMessage(msg MsgQueueData) {
	t.keyToHandleMu.Lock()
	handler, ok := t.keyToHandle[msg.Type]
	t.keyToHandleMu.Unlock()
	if !ok {
		return
	}
	handler(t, msg.Data)
}

func (t *RobotDnfTask) timerLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-t.done:
			return
		case <-ticker.C:
			t.processTimedMessages()
		}
	}
}

func (t *RobotDnfTask) processTimedMessages() {
	now := uint32(time.Now().Unix())
	t.timerMutex.Lock()
	var due []MsgQueueData
	var pending []MsgQueueData
	for _, msg := range t.messageTimerQueue {
		if msg.RunStartTime <= now {
			due = append(due, msg)
		} else {
			pending = append(pending, msg)
		}
	}
	t.messageTimerQueue = pending
	t.timerMutex.Unlock()
	for _, msg := range due {
		t.AddMessage(msg.Type, msg.Data)
	}
}

func (t *RobotDnfTask) AddMessage(typ string, data interface{}) {
	msg := MsgQueueData{Type: typ, Data: data}
	t.messageMutex.Lock()
	if len(t.messageQueue) >= maxMessageQueueSize {
		if typ == "MsgLogout" || typ == "MsgOnLine" {
			fmt.Printf("[RobotDnfTask] message_queue_full preserve_critical type=%s len=%d\n", typ, len(t.messageQueue))
			t.messageQueue[0] = msg
			t.messageMutex.Unlock()
			t.messageCond.Signal()
			return
		}
		fmt.Printf("[RobotDnfTask] message_queue_overflow drop_oldest type=%s len=%d\n", typ, len(t.messageQueue))
		t.messageQueue = t.messageQueue[1:]
	}
	t.messageQueue = append(t.messageQueue, msg)
	t.messageMutex.Unlock()
	t.messageCond.Signal()
}

func (t *RobotDnfTask) AddMessageDelay(typ string, data interface{}, sleepVal int) {
	now := uint32(time.Now().Unix())
	var runAt uint32
	if sleepVal <= 0 {
		t.AddMessage(typ, data)
		return
	}
	if sleepVal <= 86400 {
		runAt = now + uint32(sleepVal)
	} else {
		runAt = uint32(sleepVal)
	}
	if runAt <= now {
		t.AddMessage(typ, data)
		return
	}
	msg := MsgQueueData{Type: typ, Data: data, RunStartTime: runAt}
	t.timerMutex.Lock()
	if len(t.messageTimerQueue) >= maxMessageTimerQueueSize {
		fmt.Printf("[RobotDnfTask] timer_queue_overflow drop_oldest type=%s len=%d\n", typ, len(t.messageTimerQueue))
		t.messageTimerQueue = t.messageTimerQueue[1:]
	}
	t.messageTimerQueue = append(t.messageTimerQueue, msg)
	t.timerMutex.Unlock()
}

func (t *RobotDnfTask) Insert(uid uint32, vo *RobotVo) {
	t.robotVoMutex.Lock()
	defer t.robotVoMutex.Unlock()
	t.robotVoMap[int(uid)] = vo
}

func (t *RobotDnfTask) Find(uid int) *RobotVo {
	t.robotVoMutex.Lock()
	defer t.robotVoMutex.Unlock()
	return t.robotVoMap[uid]
}

func (t *RobotDnfTask) Delete(uid uint32) {
	t.robotVoMutex.Lock()
	defer t.robotVoMutex.Unlock()
	delete(t.robotVoMap, int(uid))
}

func (t *RobotDnfTask) DeleteByInt(key int) bool {
	t.robotVoMutex.Lock()
	defer t.robotVoMutex.Unlock()
	if _, ok := t.robotVoMap[key]; ok {
		delete(t.robotVoMap, key)
		return true
	}
	return true
}

func (t *RobotDnfTask) GetRobotVoMap() map[int]*RobotVo {
	t.robotVoMutex.Lock()
	defer t.robotVoMutex.Unlock()
	out := make(map[int]*RobotVo, len(t.robotVoMap))
	for k, v := range t.robotVoMap {
		out[k] = v
	}
	return out
}

func (t *RobotDnfTask) Shutdown() {
	t.shutdownOnce.Do(func() {
		close(t.done)
		t.messageCond.Broadcast()
	})
}

func GetIntRandomFromDev(min, max int) int {
	if min > max {
		min, max = max, min
	}
	return min + rand.Intn(max-min+1)
}

func (t *RobotDnfTask) dnfMsgOnLine(_ *RobotDnfTask, voVoid interface{}) bool {
	vo := voVoid.(*RobotVo)

	tmpVo := t.Find(int(vo.UID))
	if tmpVo != nil {
		if !tmpVo.CheckUserState() {
			t.DeleteByInt(int(vo.UID))
		} else {
			vo.mu.Lock()
			tasks := append([]AsyncTask(nil), vo.AfterRunAsyncTaskVec...)
			vo.mu.Unlock()
			if len(tasks) > 0 {
				tmpVo.mu.Lock()
				tmpVo.AfterRunAsyncTaskVec = tasks
				tmpVo.mu.Unlock()
				t.AddMessage("MsgOnLineAsyncTaskVec", tmpVo)
			}
			return true
		}
	}

	vo.mu.Lock()
	vo.Controller = t
	vo.IsTokenRight = false
	vo.mu.Unlock()
	go vo.Connect()

	return true
}

func (t *RobotDnfTask) dnfMsgMove(_ *RobotDnfTask, moveVoid interface{}) bool {
	md := moveVoid.(*moveInternalData)
	voObj := t.Find(md.ID)
	if voObj != nil {
		snap := voObj.Snapshot()
		if snap.Village != md.Village || snap.Area != md.Area {
			voObj.SetArea(md.Village, md.Area, md.X, md.Y)
		}
		voObj.SetPosition(md.X, md.Y, md.MoveType, md.Speed)
		return true
	}
	return false
}

func (t *RobotDnfTask) msgLogout(_ *RobotDnfTask, moveVoid interface{}) bool {
	uid := moveVoid.(int)
	voObj := t.Find(uid)
	if voObj != nil {
		t.DeleteByInt(uid)
		voObj.CloseOut()
	}
	return true
}

func (t *RobotDnfTask) msgPublicMsg(_ *RobotDnfTask, moveVoid interface{}) bool {
	md := moveVoid.(*publicMsgInternalData)
	voObj := t.Find(md.ID)
	if voObj != nil {
		msgType := md.Type
		if msgType == 0 {
			msgType = 3
		}
		voObj.SendPublicMessage(msgType, []byte(md.Msg))
	}
	return voObj != nil
}

func (t *RobotDnfTask) msgOnLineAsyncTaskVec(_ *RobotDnfTask, moveVoid interface{}) bool {
	vo := moveVoid.(*RobotVo)
	vo.mu.Lock()
	tasks := append([]AsyncTask(nil), vo.AfterRunAsyncTaskVec...)
	vo.AfterRunAsyncTaskVec = nil
	vo.mu.Unlock()

	for _, task := range tasks {
		switch task.Type {
		case AsyncMove:
		case AsyncDisjoint:
			vo.OpenDisjointStore(uint32(task.Cost))
		case AsyncPriStore:
			vo.mu.Lock()
			vo.PendingStoreTitle = task.Title
			vo.mu.Unlock()
			vo.CreatePrivateStore()
			vo.GetCompleteDisplay(0)
		}
	}
	return true
}

var _ = time.Now
