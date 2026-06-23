package service

import "time"

// actorRegistry is the narrow ownership surface used by Web/API and cleanup
// flows.
type actorRegistry interface {
	AttachUID(uid int, timeout time.Duration) bool
	Command(uid int, cmd robotActorCommand, timeout time.Duration) (RobotActionResult, bool)
	EnsureActorSlots(rc robotRuntimeConfig, target int)
	HasUID(uid int) bool
	LogoutUID(uid int, timeout time.Duration) (RobotActionResult, bool)
	StopUIDs(uids []int, logout bool) int
	actorSnapshots() []robotActorSnapshot
}

type supervisorActorRegistry struct {
	supervisor *RobotSupervisor
}

var _ actorRegistry = (*supervisorActorRegistry)(nil)

func newSupervisorActorRegistry(supervisor *RobotSupervisor) *supervisorActorRegistry {
	if supervisor == nil {
		return nil
	}
	return &supervisorActorRegistry{supervisor: supervisor}
}

// Actor ownership and command routing.

func (r *supervisorActorRegistry) Command(uid int, cmd robotActorCommand, timeout time.Duration) (RobotActionResult, bool) {
	s := r.supervisor
	actor := s.ledger.actorForUID(uid)
	if actor == nil {
		return RobotActionResult{UID: uid, OK: false, State: "missing_actor"}, false
	}
	return actor.enqueue(cmd, timeout)
}

// LogoutUID is an actor command: it logs the UID out but keeps it attached to
// its actor. Detach/release remains a scheduler ownership operation.
func (r *supervisorActorRegistry) LogoutUID(uid int, timeout time.Duration) (RobotActionResult, bool) {
	return r.Command(uid, robotActorCmdLogout, timeout)
}

// AttachUID binds an unmanaged UID to an empty actor slot. It does not perform
// login directly; callers should send robotActorCmdOnline through Command.
func (r *supervisorActorRegistry) AttachUID(uid int, timeout time.Duration) bool {
	s := r.supervisor
	actor, existing, ok := s.ledger.reserveEmptyAutoActor(uid)
	if !ok {
		return false
	}
	if existing {
		return true
	}
	if actor.assignAndWait(uid, timeout) {
		return true
	}
	s.ledger.unleaseUID(uid, actor)
	return false
}

func (r *supervisorActorRegistry) HasUID(uid int) bool {
	return r.supervisor.ledger.hasUID(uid)
}

// actorSnapshots is the read model for UI/status surfaces. Callers get a copy
// of actor pointers first so actor.snapshot() is never called while ledger is held.
func (r *supervisorActorRegistry) actorSnapshots() []robotActorSnapshot {
	s := r.supervisor
	actors := s.ledger.actorPointers()
	out := make([]robotActorSnapshot, 0, len(actors))
	for _, actor := range actors {
		out = append(out, actor.snapshot())
	}
	return out
}

// StopUID detaches the UID from supervisor ownership. With logout=true the
// actor performs release/logout before its slot is removed.
func (r *supervisorActorRegistry) StopUID(uid int, logout bool) bool {
	s := r.supervisor
	actor := s.ledger.detachUID(uid)
	if actor != nil {
		if logout {
			actor.releaseAndWait(10 * time.Second)
		}
		actor.stopAndWait(5 * time.Second)
		return true
	}
	if logout && s.runtime != nil {
		s.runtime.Logout(uid)
	}
	return false
}

// StopUIDs is the bulk ownership detach path used before cleanup/delete.
func (r *supervisorActorRegistry) StopUIDs(uids []int, logout bool) int {
	s := r.supervisor
	actors, missing := s.ledger.detachUIDs(uids)
	if logout && s.runtime != nil {
		for _, uid := range missing {
			s.runtime.Logout(uid)
		}
	}
	stopActorsConcurrent(actors, logout)
	return len(actors)
}

func (r *supervisorActorRegistry) EnsureActorSlots(rc robotRuntimeConfig, target int) {
	r.supervisor.ensureAutoActorSlots(rc, target)
}

func (m *RobotManager) currentActorRegistry() actorRegistry {
	m.autoMu.Lock()
	defer m.autoMu.Unlock()
	if m.supervisor == nil {
		return nil
	}
	return newSupervisorActorRegistry(m.supervisor)
}
