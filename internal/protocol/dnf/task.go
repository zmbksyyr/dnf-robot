package dnf

import (
	"robot/internal/foundation/lockhub"
	"sync"
)

type RobotDnfTask struct {
	messageShards [messageDispatchShards]*messageDispatchShard

	messageTimerQueue []MsgQueueData
	timerMutex        lockhub.Locker

	robotVoMap   map[int]*RobotVo
	robotVoMutex lockhub.RWLocker

	keyToHandle map[string]func(task *RobotDnfTask, data interface{}) bool

	done         chan struct{}
	shutdownOnce sync.Once

	connectQueue  chan *RobotVo
	connectMu     lockhub.Locker
	connectQueued map[uint32]struct{}
}

func NewRobotDnfTask() *RobotDnfTask {
	t := &RobotDnfTask{
		messageTimerQueue: make([]MsgQueueData, 0),
		robotVoMap:        make(map[int]*RobotVo),
		keyToHandle:       make(map[string]func(task *RobotDnfTask, data interface{}) bool),
		done:              make(chan struct{}),
		connectQueue:      make(chan *RobotVo, maxMessageQueueSize),
		connectQueued:     make(map[uint32]struct{}),
	}
	t.initKeyCall()
	for i := range t.messageShards {
		t.messageShards[i] = newMessageDispatchShard()
		go t.dispatchLoop(t.messageShards[i])
	}

	go t.connectLoop()
	go t.timerLoop()

	return t
}

func (t *RobotDnfTask) Shutdown() {
	t.shutdownOnce.Do(func() {
		close(t.done)
		for _, shard := range t.messageShards {
			shard.mu.Lock()
			shard.cond.Broadcast()
			shard.mu.Unlock()
		}
	})
}
