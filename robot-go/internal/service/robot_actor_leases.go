package service

import (
	"strings"
	"time"
)

// Lease health, broken UID cleanup, and recycle paths.

func (s *RobotSupervisor) releaseBrokenLeases() {
	leases := s.actorLedger.leaseSnapshots()
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
		if !s.actorLedger.blockLeaseIfCurrent(item.uid, item.actor) {
			continue
		}
		robotLogf("[RobotSupervisor] broken_lease uid=%d slot=%d action=release_cleanup\n", item.uid, item.actor.slotID)
		go func(actor *robotActor, uid int) {
			released := actor.releaseAndWait(10 * time.Second)
			if released != uid && released > 0 {
				s.actorLedger.unleaseUID(released, actor)
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
	s.actorLedger.unblockUID(uid)
	robotLogf("[RobotSupervisor] broken_cleanup_done uid=%d deleted=%d skipped=%d\n", uid, result.Deleted, result.Skipped)
}

func (s *RobotSupervisor) cleanupBlockedUIDs(limit int) {
	for _, uid := range s.actorLedger.blockedUIDs(limit) {
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
	s.actorLedger.removeLeaseIfActor(released, actor)
	result, err := s.manager.CleanupRobots(RobotCleanupRequest{UIDs: []int{released}, Force: true})
	if err != nil {
		robotLogf("[RobotSupervisor] recycle_cleanup_failed uid=%d err=%v\n", released, err)
		s.actorLedger.blockUID(released)
		return
	}
	robotLogf("[RobotSupervisor] recycle_cleanup_done uid=%d deleted=%d skipped=%d\n", released, result.Deleted, result.Skipped)
}
