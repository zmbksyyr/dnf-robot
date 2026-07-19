package dnf

import (
	"fmt"
	"robot/internal/foundation/lockhub"
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
		for len(shard.queue) == 0 {
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
		msg := shard.queue[0]
		shard.queue[0] = MsgQueueData{}
		shard.queue = shard.queue[1:]
		shard.mu.Unlock()

		t.handleMessage(msg)
	}
}

func (t *RobotDnfTask) handleMessage(msg MsgQueueData) {
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
	if typ == "MsgLogout" {
		shard.queue = removeQueuedUID(shard.queue, messageUID(typ, data))
	} else if typ == "MsgMove" && coalesceQueuedMove(shard.queue, data) {
		return true
	}
	if len(shard.queue) >= messageShardQueueSize {
		evict := oldestEvictableMessage(shard.queue)
		if evict < 0 {
			fmt.Printf("[RobotDnfTask] message_queue_full reject type=%s shard_len=%d\n", typ, len(shard.queue))
			return false
		}
		fmt.Printf("[RobotDnfTask] message_queue_overflow evict type=%s for=%s shard_len=%d\n", shard.queue[evict].Type, typ, len(shard.queue))
		copy(shard.queue[evict:], shard.queue[evict+1:])
		shard.queue[len(shard.queue)-1] = MsgQueueData{}
		shard.queue = shard.queue[:len(shard.queue)-1]
	}
	shard.queue = append(shard.queue, msg)
	shard.cond.Signal()
	return true
}

func messageShardIndex(typ string, data interface{}) int {
	uid := messageUID(typ, data)
	return int(uint32(uid) * 2654435761 % messageDispatchShards)
}

func messageUID(typ string, data interface{}) int {
	switch typ {
	case "MsgOnLine", "MsgReconnect", "MsgOnLineAsyncTaskVec":
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
	case "MsgOnLine", "MsgReconnect", "MsgLogout", "MsgOnLineAsyncTaskVec":
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
