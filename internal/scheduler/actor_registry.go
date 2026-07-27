package scheduler

import (
	"errors"
	"time"

	actormodel "robot/internal/actor"
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
)

var errActorRegistryUnavailable = errors.New("actor registry unavailable")

type actorRegistry interface {
	AttachUID(uid int, timeout time.Duration) bool
	Command(uid int, cmd actormodel.Command, timeout time.Duration) (robotcap.ActionResult, bool)
	EnsureActorSlots(rc robotconfig.RuntimeConfig, target int)
	HasUID(uid int) bool
	LogoutUID(uid int, timeout time.Duration) (robotcap.ActionResult, bool)
	StopUIDs(uids []int, logout bool) int
	actorSnapshots() []actormodel.Snapshot
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

func (r *supervisorActorRegistry) Command(uid int, cmd actormodel.Command, timeout time.Duration) (robotcap.ActionResult, bool) {
	s := r.supervisor
	actor := s.ledger.ActorForUID(uid)
	if actor == nil {
		return robotcap.ActionResult{UID: uid, OK: false, State: robotcap.ActionStateMissingActor}, false
	}
	return actor.Enqueue(cmd, timeout)
}

func (r *supervisorActorRegistry) LogoutUID(uid int, timeout time.Duration) (robotcap.ActionResult, bool) {
	return r.Command(uid, actormodel.CommandLogout, timeout)
}

func (r *supervisorActorRegistry) AttachUID(uid int, timeout time.Duration) bool {
	s := r.supervisor
	actor, existing, ok := s.ledger.ReserveEmptyAutoActor(uid)
	if !ok {
		return false
	}
	if existing {
		return true
	}
	if actor.AssignAndWait(uid, timeout) {
		return true
	}
	s.ledger.UnleaseUID(uid, actor)
	return false
}

func (r *supervisorActorRegistry) HasUID(uid int) bool {
	return r.supervisor.ledger.HasUID(uid)
}

func (r *supervisorActorRegistry) actorSnapshots() []actormodel.Snapshot {
	s := r.supervisor
	actors := s.ledger.ActorPointers()
	out := make([]actormodel.Snapshot, 0, len(actors))
	for _, actor := range actors {
		snapshot := actor.Snapshot()
		if s.ledger.IsDraining(actor) {
			snapshot.State = actormodel.StateReleasing
		}
		out = append(out, snapshot)
	}
	return out
}

func (r *supervisorActorRegistry) StopUID(uid int, logout bool) bool {
	s := r.supervisor
	actor := s.ledger.BeginDrainUID(uid)
	if actor != nil {
		s.stopDrainingActors([]*actormodel.Actor{actor}, actorStopWait)
		return true
	}
	if logout && s.runtime != nil {
		s.runtime.Logout(uid)
	}
	return false
}

func (r *supervisorActorRegistry) StopUIDs(uids []int, logout bool) int {
	s := r.supervisor
	actors, missing := s.ledger.BeginDrainUIDs(uids)
	if logout && s.runtime != nil {
		for _, uid := range missing {
			if !s.runtime.IsActive(uid) {
				continue
			}
			s.runtime.Logout(uid)
		}
	}
	s.stopDrainingActors(actors, actorStopWait)
	return len(actors)
}

func (r *supervisorActorRegistry) EnsureActorSlots(rc robotconfig.RuntimeConfig, target int) {
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
