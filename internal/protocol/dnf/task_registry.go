package dnf

import (
	"fmt"
	"time"
)

const (
	connectLaunchInterval = 35 * time.Millisecond
	maxConcurrentConnects = 32
)

func (t *RobotDnfTask) connectLoop() {
	ticker := time.NewTicker(connectLaunchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-t.done:
			return
		case vo := <-t.connectQueue:
			t.connectMu.Lock()
			if t.connectQueued[vo.UID] != vo {
				t.connectMu.Unlock()
				continue
			}
			delete(t.connectQueued, vo.UID)
			t.connectMu.Unlock()
			select {
			case <-t.done:
				return
			case <-ticker.C:
			}
			select {
			case <-t.done:
				return
			case t.connectSlots <- struct{}{}:
			}
			t.workers.Add(1)
			go func(vo *RobotVo) {
				defer t.workers.Done()
				defer func() { <-t.connectSlots }()
				defer func() {
					if rec := recover(); rec != nil {
						fmt.Printf("[RobotDnfTask] connect_panic uid=%d err=%v\n", vo.UID, rec)
						func() {
							defer func() { _ = recover() }()
							vo.CloseOut()
						}()
					}
				}()
				vo.Connect()
			}(vo)
		}
	}
}

func (t *RobotDnfTask) replaceCurrent(uid uint32, current, replacement *RobotVo) bool {
	if replacement == nil {
		return false
	}
	t.robotVoMutex.Lock()
	defer t.robotVoMutex.Unlock()
	select {
	case <-t.done:
		return false
	default:
	}
	if t.robotVoMap[int(uid)] != current {
		return false
	}
	t.robotVoMap[int(uid)] = replacement
	return true
}

func (t *RobotDnfTask) Find(uid int) *RobotVo {
	t.robotVoMutex.RLock()
	defer t.robotVoMutex.RUnlock()
	return t.robotVoMap[uid]
}

func (t *RobotDnfTask) DeleteIf(uid uint32, vo *RobotVo) bool {
	t.robotVoMutex.Lock()
	defer t.robotVoMutex.Unlock()
	if t.robotVoMap[int(uid)] != vo {
		return false
	}
	delete(t.robotVoMap, int(uid))
	return true
}

func (t *RobotDnfTask) isCurrent(uid uint32, vo *RobotVo) bool {
	t.robotVoMutex.RLock()
	defer t.robotVoMutex.RUnlock()
	return t.robotVoMap[int(uid)] == vo
}

func (t *RobotDnfTask) GetRobotVoMap() map[int]*RobotVo {
	t.robotVoMutex.RLock()
	defer t.robotVoMutex.RUnlock()
	out := make(map[int]*RobotVo, len(t.robotVoMap))
	for k, v := range t.robotVoMap {
		out[k] = v
	}
	return out
}

func (t *RobotDnfTask) enqueueConnect(vo *RobotVo) bool {
	if vo == nil {
		return false
	}
	t.connectMu.Lock()
	select {
	case <-t.done:
		t.connectMu.Unlock()
		return false
	default:
	}
	if queued := t.connectQueued[vo.UID]; queued == vo {
		t.connectMu.Unlock()
		return true
	}
	select {
	case t.connectQueue <- vo:
		t.connectQueued[vo.UID] = vo
		t.connectMu.Unlock()
		return true
	default:
		t.connectMu.Unlock()
		fmt.Printf("[RobotDnfTask] connect_queue_full uid=%d len=%d\n", vo.UID, len(t.connectQueue))
		return false
	}
}

func (t *RobotDnfTask) onlineBacklog() int {
	pendingMessages := 0
	for _, shard := range t.messageShards {
		shard.mu.Lock()
		for _, msg := range shard.queue {
			if msg.Type == "MsgOnLine" {
				pendingMessages++
			}
		}
		shard.mu.Unlock()
	}
	return pendingMessages + len(t.connectQueue)
}
