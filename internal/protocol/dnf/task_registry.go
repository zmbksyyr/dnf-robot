package dnf

import (
	"fmt"
	"time"
)

const connectLaunchInterval = 35 * time.Millisecond

func (t *RobotDnfTask) connectLoop() {
	for {
		select {
		case <-t.done:
			return
		case vo := <-t.connectQueue:
			t.connectMu.Lock()
			delete(t.connectQueued, vo.UID)
			t.connectMu.Unlock()
			select {
			case <-t.done:
				return
			case <-time.After(connectLaunchInterval):
				go vo.Connect()
			}
		}
	}
}

func (t *RobotDnfTask) Insert(uid uint32, vo *RobotVo) {
	t.robotVoMutex.Lock()
	defer t.robotVoMutex.Unlock()
	t.robotVoMap[int(uid)] = vo
}

func (t *RobotDnfTask) Find(uid int) *RobotVo {
	t.robotVoMutex.RLock()
	defer t.robotVoMutex.RUnlock()
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
	return false
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
	if _, exists := t.connectQueued[vo.UID]; exists {
		t.connectMu.Unlock()
		return true
	}
	select {
	case t.connectQueue <- vo:
		t.connectQueued[vo.UID] = struct{}{}
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
