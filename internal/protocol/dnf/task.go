package dnf

import (
	"context"
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
	ctx          context.Context
	cancel       context.CancelFunc
	workers      sync.WaitGroup

	connectQueue  chan *RobotVo
	connectSlots  chan struct{}
	connectMu     lockhub.Locker
	connectQueued map[uint32]*RobotVo
}

func NewRobotDnfTask() *RobotDnfTask {
	ctx, cancel := context.WithCancel(context.Background())
	t := &RobotDnfTask{
		messageTimerQueue: make([]MsgQueueData, 0),
		robotVoMap:        make(map[int]*RobotVo),
		keyToHandle:       make(map[string]func(task *RobotDnfTask, data interface{}) bool),
		done:              make(chan struct{}),
		ctx:               ctx,
		cancel:            cancel,
		connectQueue:      make(chan *RobotVo, maxMessageQueueSize),
		connectSlots:      make(chan struct{}, maxConcurrentConnects),
		connectQueued:     make(map[uint32]*RobotVo),
	}
	t.initKeyCall()
	for i := range t.messageShards {
		t.messageShards[i] = newMessageDispatchShard()
		t.workers.Add(1)
		go func(shard *messageDispatchShard) {
			defer t.workers.Done()
			t.dispatchLoop(shard)
		}(t.messageShards[i])
	}

	t.workers.Add(2)
	go func() {
		defer t.workers.Done()
		t.connectLoop()
	}()
	go func() {
		defer t.workers.Done()
		t.timerLoop()
	}()

	return t
}

func (t *RobotDnfTask) Shutdown() {
	t.shutdownOnce.Do(func() {
		if t.cancel != nil {
			t.cancel()
		}
		close(t.done)

		t.timerMutex.Lock()
		for i := range t.messageTimerQueue {
			t.messageTimerQueue[i] = MsgQueueData{}
		}
		t.messageTimerQueue = nil
		t.timerMutex.Unlock()

		for _, shard := range t.messageShards {
			shard.mu.Lock()
			for i := range shard.queue {
				shard.queue[i] = MsgQueueData{}
			}
			shard.queue = nil
			shard.cond.Broadcast()
			shard.mu.Unlock()
		}

		t.connectMu.Lock()
		clear(t.connectQueued)
		t.connectMu.Unlock()

		t.robotVoMutex.Lock()
		robots := make([]*RobotVo, 0, len(t.robotVoMap))
		for uid, vo := range t.robotVoMap {
			robots = append(robots, vo)
			delete(t.robotVoMap, uid)
		}
		t.robotVoMutex.Unlock()
		for _, vo := range robots {
			vo.CloseOut()
		}
		t.workers.Wait()
	})
}

func (t *RobotDnfTask) context() context.Context {
	if t == nil || t.ctx == nil {
		return context.Background()
	}
	return t.ctx
}
