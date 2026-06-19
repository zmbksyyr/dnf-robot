package service

import (
	"strings"
	"time"
)

// Lease health, broken UID cleanup, and recycle paths.

func (s *RobotSupervisor) releaseBrokenLeases() {
	type lease struct {
		uid   int
		actor *robotActor
	}
	var leases []lease
	s.mu.Lock()
	for uid, actor := range s.uidActors {
		if uid > 0 && actor != nil {
			leases = append(leases, lease{uid: uid, actor: actor})
		}
	}
	s.mu.Unlock()
	if len(leases) == 0 {
		return
	}
	uids := make([]int, 0, len(leases))
	for _, item := range leases {
		uids = append(uids, item.uid)
	}
	alive, err := s.manager.aliveRobotUIDs(uids)
	if err != nil {
		robotLogf("[RobotSupervisor] lease_health_check_failed err=%v\n", err)
		return
	}
	for _, item := range leases {
		if alive[item.uid] {
			continue
		}
		s.mu.Lock()
		if s.uidActors[item.uid] != item.actor {
			s.mu.Unlock()
			continue
		}
		delete(s.uidActors, item.uid)
		s.blockedUID[item.uid] = struct{}{}
		s.mu.Unlock()
		robotLogf("[RobotSupervisor] broken_lease uid=%d slot=%d action=release_cleanup\n", item.uid, item.actor.slotID)
		go func(actor *robotActor, uid int) {
			released := actor.releaseAndWait(10 * time.Second)
			if released != uid && released > 0 {
				s.unleaseUID(released, actor)
			}
			s.cleanupBrokenUID(uid)
		}(item.actor, item.uid)
	}
}

func (s *RobotSupervisor) cleanupBrokenUID(uid int) {
	if uid <= 0 {
		return
	}
	result, err := s.manager.CleanupRobots(RobotCleanupRequest{UIDs: []int{uid}, Force: true, InternalConfirmedBroken: true})
	if err != nil {
		robotLogf("[RobotSupervisor] broken_cleanup_failed uid=%d err=%v\n", uid, err)
		return
	}
	if result.Deleted <= 0 {
		robotLogf("[RobotSupervisor] broken_cleanup_skipped uid=%d requested=%d skipped=%d\n", uid, result.Requested, result.Skipped)
		return
	}
	s.mu.Lock()
	delete(s.blockedUID, uid)
	s.mu.Unlock()
	robotLogf("[RobotSupervisor] broken_cleanup_done uid=%d deleted=%d skipped=%d\n", uid, result.Deleted, result.Skipped)
}

func (s *RobotSupervisor) cleanupBlockedUIDs(limit int) {
	if limit <= 0 {
		return
	}
	var uids []int
	s.mu.Lock()
	for uid := range s.blockedUID {
		uids = append(uids, uid)
		if len(uids) >= limit {
			break
		}
	}
	s.mu.Unlock()
	for _, uid := range uids {
		s.cleanupBrokenUID(uid)
	}
}

func (m *RobotManager) aliveRobotUIDs(uids []int) (map[int]bool, error) {
	alive := make(map[int]bool, len(uids))
	if len(uids) == 0 {
		return alive, nil
	}
	holders := strings.TrimRight(strings.Repeat("?,", len(uids)), ",")
	args := make([]interface{}, len(uids))
	for i, uid := range uids {
		args[i] = uid
	}
	rows, err := m.db.Query("SELECT m_id FROM taiwan_cain.charac_info WHERE delete_flag=0 AND m_id IN ("+holders+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var uid int
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		alive[uid] = true
	}
	return alive, rows.Err()
}

func (s *RobotSupervisor) recycleActorUID(actor *robotActor, status robotActorStatus) {
	if status.UID <= 0 {
		return
	}
	robotLogf("[RobotSupervisor] recycle_uid slot=%d uid=%d health=%s reason=%s failures=%d first_failure=%s state=%s\n",
		status.SlotID, status.UID, status.Health, status.HealthReason, status.Failures, status.FirstFailureAt.Format(time.RFC3339), status.State)
	released := actor.releaseAndWait(15 * time.Second)
	if released <= 0 {
		released = status.UID
	}
	s.mu.Lock()
	if s.uidActors[released] == actor {
		delete(s.uidActors, released)
	}
	s.mu.Unlock()
	result, err := s.manager.CleanupRobots(RobotCleanupRequest{UIDs: []int{released}, Force: true})
	if err != nil {
		robotLogf("[RobotSupervisor] recycle_cleanup_failed uid=%d err=%v\n", released, err)
		s.mu.Lock()
		s.blockedUID[released] = struct{}{}
		s.mu.Unlock()
		return
	}
	robotLogf("[RobotSupervisor] recycle_cleanup_done uid=%d deleted=%d skipped=%d\n", released, result.Deleted, result.Skipped)
}

func (s *RobotSupervisor) blockedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.blockedUID)
}

func (s *RobotSupervisor) tryLeaseUID(uid int, actor *robotActor) bool {
	if uid <= 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, blocked := s.blockedUID[uid]; blocked {
		return false
	}
	if _, leased := s.uidActors[uid]; leased {
		return false
	}
	s.uidActors[uid] = actor
	return true
}

func (s *RobotSupervisor) unleaseUID(uid int, actor *robotActor) {
	if uid <= 0 {
		return
	}
	s.mu.Lock()
	if actor == nil || s.uidActors[uid] == actor || s.uidActors[uid] == nil {
		delete(s.uidActors, uid)
	}
	s.mu.Unlock()
}
