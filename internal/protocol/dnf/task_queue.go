package dnf

import (
	"fmt"
	"robot/internal/foundation/lockhub"
	"runtime/debug"
	"sync"
	"time"
)

type MsgQueueData struct {
	Type         string
	Data         interface{}
	RunStartTime uint32
}

const (
	maxMessageQueueSize      = 5000
	maxMessageTimerQueueSize = 10000
	messageDispatchShards    = 32
	messageShardQueueSize    = (maxMessageQueueSize + messageDispatchShards - 1) / messageDispatchShards
)

type messageDispatchShard struct {
	queue []MsgQueueData
	head  int
	mu    lockhub.Locker
	cond  *sync.Cond
}

func newMessageDispatchShard() *messageDispatchShard {
	shard := &messageDispatchShard{queue: make([]MsgQueueData, 0, messageShardQueueSize)}
	shard.cond = sync.NewCond(&shard.mu)
	return shard
}

func (t *RobotDnfTask) dispatchLoop(shard *messageDispatchShard) {
	for {
		shard.mu.Lock()
		for shard.head >= len(shard.queue) {
			select {
			case <-t.done:
				shard.mu.Unlock()
				return
			default:
			}
			shard.cond.Wait()
		}
		select {
		case <-t.done:
			shard.mu.Unlock()
			return
		default:
		}
		msg := shard.queue[shard.head]
		shard.queue[shard.head] = MsgQueueData{}
		shard.head++
		shard.compactIfNeeded(false)
		shard.mu.Unlock()

		t.handleMessage(msg)
	}
}

func (t *RobotDnfTask) handleMessage(msg MsgQueueData) {
	defer func() {
		if rec := recover(); rec != nil {
			fmt.Printf("[RobotDnfTask] message_panic type=%s err=%v\n%s", msg.Type, rec, debug.Stack())
		}
	}()
	handler, ok := t.keyToHandle[msg.Type]
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
	t.TryAddMessage(typ, data)
}

func (t *RobotDnfTask) TryAddMessage(typ string, data interface{}) bool {
	select {
	case <-t.done:
		return false
	default:
	}
	msg := MsgQueueData{Type: typ, Data: data}
	shard := t.messageShards[messageShardIndex(typ, data)]
	shard.mu.Lock()
	defer shard.mu.Unlock()
	select {
	case <-t.done:
		return false
	default:
	}
	if typ == "MsgLogout" || typ == "MsgOnLine" {
		shard.queue = removeQueuedUID(shard.liveQueue(), messageUID(typ, data))
		shard.head = 0
	} else if typ == "MsgMove" && coalesceQueuedMove(shard.liveQueue(), data) {
		return true
	}
	shard.compactIfNeeded(true)
	if shard.liveLen() >= messageShardQueueSize {
		live := shard.liveQueue()
		evict := oldestEvictableMessage(live)
		if evict < 0 {
			fmt.Printf("[RobotDnfTask] message_queue_full reject type=%s shard_len=%d\n", typ, len(live))
			return false
		}
		fmt.Printf("[RobotDnfTask] message_queue_overflow evict type=%s for=%s shard_len=%d\n", live[evict].Type, typ, len(live))
		copy(live[evict:], live[evict+1:])
		live[len(live)-1] = MsgQueueData{}
		shard.queue = live[:len(live)-1]
		shard.head = 0
	}
	shard.queue = append(shard.queue, msg)
	shard.cond.Signal()
	return true
}

// dispatchOnlineImmediate removes stale work for this UID and crosses the
// lifecycle boundary synchronously. Disjoint preparation already performed a
// direct close and cache-release wait, so queueing here would only allow old
// movement/login work to overtake CMD 238 preparation.
func (t *RobotDnfTask) dispatchOnlineImmediate(vo *RobotVo) bool {
	if t == nil || vo == nil {
		return false
	}
	select {
	case <-t.done:
		return false
	default:
	}
	shard := t.messageShards[messageShardIndex("MsgOnLine", vo)]
	shard.mu.Lock()
	shard.queue = removeQueuedUID(shard.liveQueue(), int(vo.UID))
	shard.head = 0
	shard.mu.Unlock()
	return t.dnfMsgOnLine(t, vo)
}

func (s *messageDispatchShard) liveQueue() []MsgQueueData {
	if s.head >= len(s.queue) {
		return s.queue[:0]
	}
	return s.queue[s.head:]
}

func (s *messageDispatchShard) liveLen() int {
	return len(s.queue) - s.head
}

func (s *messageDispatchShard) compactIfNeeded(needTail bool) {
	if s.head == 0 {
		return
	}
	if !needTail && (s.head < 64 || s.head*2 < len(s.queue)) {
		return
	}
	if needTail && len(s.queue) < cap(s.queue) && (s.head < 64 || s.head*2 < len(s.queue)) {
		return
	}
	live := copy(s.queue, s.queue[s.head:])
	for i := live; i < len(s.queue); i++ {
		s.queue[i] = MsgQueueData{}
	}
	s.queue = s.queue[:live]
	s.head = 0
}

func messageShardIndex(typ string, data interface{}) int {
	uid := messageUID(typ, data)
	return int(uint32(uid) * 2654435761 % messageDispatchShards)
}

func messageUID(typ string, data interface{}) int {
	switch typ {
	case "MsgOnLine", "MsgReconnect":
		if vo, ok := data.(*RobotVo); ok && vo != nil {
			return int(vo.UID)
		}
	case "MsgMove":
		if move, ok := data.(*moveInternalData); ok && move != nil {
			return move.ID
		}
	case "MsgLogout":
		uid, _ := data.(int)
		return uid
	case "MsgPublicMsg":
		if msg, ok := data.(*publicMsgInternalData); ok && msg != nil {
			return msg.ID
		}
	}
	return 0
}

func lifecycleMessage(typ string) bool {
	switch typ {
	case "MsgOnLine", "MsgReconnect", "MsgLogout":
		return true
	default:
		return false
	}
}

func removeQueuedUID(queue []MsgQueueData, uid int) []MsgQueueData {
	if uid <= 0 || len(queue) == 0 {
		return queue
	}
	kept := queue[:0]
	for _, queued := range queue {
		if messageUID(queued.Type, queued.Data) == uid {
			continue
		}
		kept = append(kept, queued)
	}
	for i := len(kept); i < len(queue); i++ {
		queue[i] = MsgQueueData{}
	}
	return kept
}

func coalesceQueuedMove(queue []MsgQueueData, data interface{}) bool {
	uid := messageUID("MsgMove", data)
	if uid <= 0 {
		return false
	}
	for i := len(queue) - 1; i >= 0; i-- {
		queuedUID := messageUID(queue[i].Type, queue[i].Data)
		if queuedUID != uid {
			continue
		}
		if lifecycleMessage(queue[i].Type) {
			return false
		}
		if queue[i].Type == "MsgMove" {
			queue[i].Data = data
			return true
		}
	}
	return false
}

func oldestEvictableMessage(queue []MsgQueueData) int {
	for i := range queue {
		if !lifecycleMessage(queue[i].Type) {
			return i
		}
	}
	return -1
}

func (t *RobotDnfTask) AddMessageDelay(typ string, data interface{}, sleepVal int) {
	select {
	case <-t.done:
		return
	default:
	}
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
	select {
	case <-t.done:
		t.timerMutex.Unlock()
		return
	default:
	}
	if len(t.messageTimerQueue) >= maxMessageTimerQueueSize {
		fmt.Printf("[RobotDnfTask] timer_queue_overflow drop_oldest type=%s len=%d\n", typ, len(t.messageTimerQueue))
		t.messageTimerQueue = t.messageTimerQueue[1:]
	}
	t.messageTimerQueue = append(t.messageTimerQueue, msg)
	t.timerMutex.Unlock()
}
