package service

import "time"

type RobotSupervisor struct {
	manager *RobotManager
	runtime *RobotRuntime

	ledger actorLedger

	stop chan struct{}
	done chan struct{}

	nextMetrics time.Time
	nextKeyLog  time.Time
}

// RobotSupervisor coordinates the runtime loop. Actor ownership is stored in
// the ledger and exposed through the registry boundary; automatic scheduling
// lives in robot_auto_scheduler.go.
func NewRobotSupervisor(manager *RobotManager, runtime *RobotRuntime) *RobotSupervisor {
	return &RobotSupervisor{
		manager: manager,
		runtime: runtime,
		ledger:  newActorLedger(),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

// Lifecycle.

func (s *RobotSupervisor) Start() {
	go s.loop()
}

func (s *RobotSupervisor) Stop() {
	close(s.stop)
	<-s.done
}
