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
	if len(shard.queue) >= messageShardQueueSize {
		if typ == "MsgOnLine" {
			fmt.Printf("[RobotDnfTask] message_queue_full reject_online shard_len=%d\n", len(shard.queue))
			return false
		}
		if typ == "MsgLogout" {
			fmt.Printf("[RobotDnfTask] message_queue_full enqueue_logout_drop_oldest shard_len=%d\n", len(shard.queue))
		} else {
			fmt.Printf("[RobotDnfTask] message_queue_overflow drop_oldest type=%s shard_len=%d\n", typ, len(shard.queue))
		}
		shard.queue[0] = MsgQueueData{}
		shard.queue = shard.queue[1:]
	}
	shard.queue = append(shard.queue, msg)
	shard.cond.Signal()
	return true
}

func messageShardIndex(typ string, data interface{}) int {
	uid := 0
	switch typ {
	case "MsgOnLine", "MsgReconnect", "MsgOnLineAsyncTaskVec":
		if vo, ok := data.(*RobotVo); ok && vo != nil {
			uid = int(vo.UID)
		}
	case "MsgMove":
		if move, ok := data.(*moveInternalData); ok && move != nil {
			uid = move.ID
		}
	case "MsgLogout":
		uid, _ = data.(int)
	case "MsgPublicMsg":
		if msg, ok := data.(*publicMsgInternalData); ok && msg != nil {
			uid = msg.ID
		}
	}
	return int(uint32(uid) * 2654435761 % messageDispatchShards)
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
