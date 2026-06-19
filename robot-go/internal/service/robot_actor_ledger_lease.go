package service

func (l *actorLedger) blockLeaseIfCurrent(uid int, actor *robotActor) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.uidActors[uid] != actor {
		return false
	}
	delete(l.uidActors, uid)
	l.blockedUID[uid] = struct{}{}
	return true
}

func (l *actorLedger) unblockUID(uid int) {
	l.mu.Lock()
	delete(l.blockedUID, uid)
	l.mu.Unlock()
}

func (l *actorLedger) blockedUIDs(limit int) []int {
	if limit <= 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	uids := make([]int, 0, limit)
	for uid := range l.blockedUID {
		uids = append(uids, uid)
		if len(uids) >= limit {
			break
		}
	}
	return uids
}

func (l *actorLedger) blockedCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.blockedUID)
}

func (l *actorLedger) tryLeaseUID(uid int, actor *robotActor) bool {
	if uid <= 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, blocked := l.blockedUID[uid]; blocked {
		return false
	}
	if _, leased := l.uidActors[uid]; leased {
		return false
	}
	l.uidActors[uid] = actor
	return true
}

func (l *actorLedger) unleaseUID(uid int, actor *robotActor) {
	if uid <= 0 {
		return
	}
	l.mu.Lock()
	if actor == nil || l.uidActors[uid] == actor || l.uidActors[uid] == nil {
		delete(l.uidActors, uid)
	}
	l.mu.Unlock()
}

func (l *actorLedger) removeLeaseIfActor(uid int, actor *robotActor) {
	l.mu.Lock()
	if l.uidActors[uid] == actor {
		delete(l.uidActors, uid)
	}
	l.mu.Unlock()
}

func (l *actorLedger) blockUID(uid int) {
	if uid <= 0 {
		return
	}
	l.mu.Lock()
	l.blockedUID[uid] = struct{}{}
	l.mu.Unlock()
}
