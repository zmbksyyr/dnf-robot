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
