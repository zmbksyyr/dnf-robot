package service

import "time"

// actorRegistry is the narrow ownership surface used by Web/API and cleanup
// flows. RobotSupervisor implements it today; keeping callers on this interface
// makes a later ActorRegistry extraction mechanical.
type actorRegistry interface {
	AttachUID(uid int, timeout time.Duration) bool
	Command(uid int, cmd robotActorCommand, timeout time.Duration) (RobotActionResult, bool)
	EnsureActorSlots(rc robotRuntimeConfig, target int)
	HasUID(uid int) bool
	LogoutUID(uid int, timeout time.Duration) (RobotActionResult, bool)
	StopUIDs(uids []int, logout bool) int
	actorSnapshots() []robotActorSnapshot
}

var _ actorRegistry = (*RobotSupervisor)(nil)

// Actor ownership and command routing.

func (s *RobotSupervisor) Command(uid int, cmd robotActorCommand, timeout time.Duration) (RobotActionResult, bool) {
	s.mu.Lock()
	actor := s.uidActors[uid]
	s.mu.Unlock()
	if actor == nil {
		return RobotActionResult{UID: uid, OK: false, State: "missing_actor"}, false
	}
	return actor.enqueue(cmd, timeout)
}

// LogoutUID is an actor command: it logs the UID out but keeps it attached to
// its actor. Detach/release remains a scheduler ownership operation.
func (s *RobotSupervisor) LogoutUID(uid int, timeout time.Duration) (RobotActionResult, bool) {
	return s.Command(uid, robotActorCmdLogout, timeout)
}

// AttachUID binds an unmanaged UID to an empty actor slot. It does not perform
// login directly; callers should send robotActorCmdOnline through Command.
func (s *RobotSupervisor) AttachUID(uid int, timeout time.Duration) bool {
	if uid <= 0 {
		return false
	}
	var actor *robotActor
	s.mu.Lock()
	if existing := s.uidActors[uid]; existing != nil {
		s.mu.Unlock()
		return true
	}
	if _, blocked := s.blockedUID[uid]; blocked {
		s.mu.Unlock()
		return false
	}
	for _, candidate := range s.actors {
		snap := candidate.snapshot()
		if snap.Mode == robotActorAuto && snap.UID <= 0 {
			actor = candidate
			break
		}
	}
	if actor != nil {
		s.uidActors[uid] = actor
	}
	s.mu.Unlock()
	if actor == nil {
		return false
	}
	if actor.assignAndWait(uid, timeout) {
		return true
	}
	s.actorLedger.unleaseUID(uid, actor)
	return false
}

func (s *RobotSupervisor) HasUID(uid int) bool {
	if uid <= 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.uidActors[uid] != nil
}

// actorSnapshots is the read model for UI/status surfaces. Callers get a copy
// of actor pointers first so actor.snapshot() is never called while s.mu is held.
func (s *RobotSupervisor) actorSnapshots() []robotActorSnapshot {
	actors := s.actorLedger.actorPointers()
	out := make([]robotActorSnapshot, 0, len(actors))
	for _, actor := range actors {
		out = append(out, actor.snapshot())
	}
	return out
}

// StopUID detaches the UID from supervisor ownership. With logout=true the
// actor performs release/logout before its slot is removed.
func (s *RobotSupervisor) StopUID(uid int, logout bool) bool {
	s.mu.Lock()
	actor := s.uidActors[uid]
	if actor != nil {
		delete(s.uidActors, uid)
		delete(s.actors, actor.slotID)
	}
	s.mu.Unlock()
	if actor != nil {
		if logout {
			actor.releaseAndWait(10 * time.Second)
		}
		actor.stopAndWait(5 * time.Second)
		return true
	}
	if logout {
		s.runtime.Logout(uid)
	}
	return false
}

// StopUIDs is the bulk ownership detach path used before cleanup/delete.
func (s *RobotSupervisor) StopUIDs(uids []int, logout bool) int {
	if len(uids) == 0 {
		return 0
	}
	seen := make(map[int]struct{}, len(uids))
	actors := make([]*robotActor, 0, len(uids))
	missing := make([]int, 0)
	s.mu.Lock()
	for _, uid := range uids {
		if uid <= 0 {
			continue
		}
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		actor := s.uidActors[uid]
		if actor == nil {
			if logout {
				missing = append(missing, uid)
			}
			continue
		}
		delete(s.uidActors, uid)
		delete(s.actors, actor.slotID)
		delete(s.blockedUID, uid)
		actors = append(actors, actor)
	}
	s.mu.Unlock()
	if s.runtime != nil {
		for _, uid := range missing {
			s.runtime.Logout(uid)
		}
	}
	stopActorsConcurrent(actors, logout)
	return len(actors)
}

func (s *RobotSupervisor) EnsureActorSlots(rc robotRuntimeConfig, target int) {
	s.ensureAutoActorSlots(rc, target)
}

func (m *RobotManager) currentActorRegistry() actorRegistry {
	m.autoMu.Lock()
	defer m.autoMu.Unlock()
	if m.supervisor == nil {
		return nil
	}
	return m.supervisor
}

func (m *RobotManager) currentSupervisor() *RobotSupervisor {
	m.autoMu.Lock()
	defer m.autoMu.Unlock()
	return m.supervisor
}
