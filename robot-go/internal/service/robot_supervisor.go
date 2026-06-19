package service

import (
	"sync"
	"time"
)

type RobotSupervisor struct {
	manager *RobotManager
	runtime *RobotRuntime

	mu         sync.Mutex
	actors     map[int]*robotActor
	uidActors  map[int]*robotActor
	blockedUID map[int]struct{}
	nextSlotID int
	stop       chan struct{}
	done       chan struct{}

	nextMetrics time.Time
	nextKeyLog  time.Time
}

// RobotSupervisor is the runtime coordinator that holds actor ownership state.
// Ownership commands live in robot_actor_registry.go; automatic scheduling lives
// in robot_auto_scheduler.go.
func NewRobotSupervisor(manager *RobotManager, runtime *RobotRuntime) *RobotSupervisor {
	return &RobotSupervisor{
		manager:    manager,
		runtime:    runtime,
		actors:     make(map[int]*robotActor),
		uidActors:  make(map[int]*robotActor),
		blockedUID: make(map[int]struct{}),
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
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
