package scheduler

import (
	"sync"
	"time"

	actormodel "robot/internal/actor"
	"robot/internal/foundation/lockhub"
)

type RobotSupervisor struct {
	manager *RobotManager
	runtime actormodel.RobotRuntime

	ledger actormodel.Ledger

	stop   chan struct{}
	done   chan struct{}
	once   sync.Once
	stopWG sync.WaitGroup

	pressureMu      lockhub.Locker
	pressureRunning bool

	nextMetrics      time.Time
	nextKeyLog       time.Time
	nextLeaseHealth  time.Time
	nextAnnouncement time.Time
}

func NewRobotSupervisor(manager *RobotManager, runtime actormodel.RobotRuntime) *RobotSupervisor {
	return &RobotSupervisor{
		manager: manager,
		runtime: runtime,
		ledger:  actormodel.NewLedger(),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

func (s *RobotSupervisor) Start() {
	go s.loop()
}

func (s *RobotSupervisor) Stop() {
	s.once.Do(func() { close(s.stop) })
	<-s.done
}

func (s *RobotSupervisor) loop() {
	defer close(s.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			s.stopAll(true)
			s.stopWG.Wait()
			return
		case now := <-ticker.C:
			s.tick(now)
		}
	}
}

func (s *RobotSupervisor) tick(now time.Time) {
	rc := s.manager.loadRobotConfig()
	s.sendSystemAnnouncementIfDue(now)
	if s.handleAutoGuards(now, rc) {
		return
	}
	s.maintainTarget(rc)
	s.releaseBrokenLeases(now, rc)
	s.cleanupBlockedUIDs(10)
	s.recycleUnhealthyActors(now, rc)
	s.assignIdleAutoActors(rc)
	s.updateMetrics(rc)
}
