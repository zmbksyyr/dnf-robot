package dnfruntime

import "robot/internal/shared"

func (rs *RobotSvc) enqueue(entry robotMsgEntry) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if !rs.running || rs.table == nil {
		return ErrRuntimeStopped
	}

	switch entry.typ {
	case robotCommandLogout:
		rs.msgQueue = removeQueuedRobotUID(rs.msgQueue, entry.uid)
	case robotCommandMove:
		if coalesceQueuedRobotMove(rs.msgQueue, entry) {
			return nil
		}
	}

	if len(rs.msgQueue) >= maxRobotCommandQueue {
		evict := oldestEvictableRobotCommand(rs.msgQueue)
		if evict < 0 {
			robotLogf("[RobotSvc] command_queue_full type=%s len=%d\n", entry.typ, maxRobotCommandQueue)
			return ErrCommandQueueFull
		}
		copy(rs.msgQueue[evict:], rs.msgQueue[evict+1:])
		rs.msgQueue[len(rs.msgQueue)-1] = robotMsgEntry{}
		rs.msgQueue = rs.msgQueue[:len(rs.msgQueue)-1]
	}
	rs.msgQueue = append(rs.msgQueue, entry)
	rs.cond.Signal()
	return nil
}

func removeQueuedRobotUID(queue []robotMsgEntry, uid int) []robotMsgEntry {
	if uid <= 0 || len(queue) == 0 {
		return queue
	}
	kept := queue[:0]
	for _, entry := range queue {
		if entry.typ == robotCommandOnline {
			entry.online = withoutOnlineUID(entry.online, uid)
			if len(entry.online) > 0 {
				kept = append(kept, entry)
			}
			continue
		}
		if robotCommandUID(entry) != uid {
			kept = append(kept, entry)
		}
	}
	for index := len(kept); index < len(queue); index++ {
		queue[index] = robotMsgEntry{}
	}
	return kept
}

func withoutOnlineUID(users []shared.RuntimeOnlineUser, uid int) []shared.RuntimeOnlineUser {
	kept := users[:0]
	for _, user := range users {
		if user.UID != uid {
			kept = append(kept, user)
		}
	}
	for index := len(kept); index < len(users); index++ {
		users[index] = shared.RuntimeOnlineUser{}
	}
	return kept
}

func coalesceQueuedRobotMove(queue []robotMsgEntry, replacement robotMsgEntry) bool {
	uid := replacement.move.UID
	if uid <= 0 {
		return false
	}
	for index := len(queue) - 1; index >= 0; index-- {
		entry := queue[index]
		if !robotCommandContainsUID(entry, uid) {
			continue
		}
		if entry.typ == robotCommandOnline || entry.typ == robotCommandLogout {
			return false
		}
		if entry.typ == robotCommandMove {
			queue[index] = replacement
			return true
		}
	}
	return false
}

func robotCommandContainsUID(entry robotMsgEntry, uid int) bool {
	if entry.typ == robotCommandOnline {
		for _, user := range entry.online {
			if user.UID == uid {
				return true
			}
		}
		return false
	}
	return robotCommandUID(entry) == uid
}

func robotCommandUID(entry robotMsgEntry) int {
	switch entry.typ {
	case robotCommandLogout:
		return entry.uid
	case robotCommandMove:
		return entry.move.UID
	case robotCommandShout:
		return entry.shout.UID
	default:
		return 0
	}
}

func oldestEvictableRobotCommand(queue []robotMsgEntry) int {
	for index, entry := range queue {
		if entry.typ == robotCommandMove || entry.typ == robotCommandShout {
			return index
		}
	}
	return -1
}
