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

	stop chan struct{}
	done chan struct{}
	once sync.Once

	shutdownMu         lockhub.Locker
	shutdownErr        error
	shutdownTimeout    time.Duration
	shutdownForceGrace time.Duration

	pressureMu      lockhub.Locker
	pressureRunning bool
	pressureDone    chan struct{}

	nextMetrics      time.Time
	nextKeyLog       time.Time
	nextLeaseHealth  time.Time
	nextAnnouncement time.Time
}

func NewRobotSupervisor(manager *RobotManager, runtime actormodel.RobotRuntime) *RobotSupervisor {
	return &RobotSupervisor{
		manager:            manager,
		runtime:            runtime,
		ledger:             actormodel.NewLedger(),
		stop:               make(chan struct{}),
		done:               make(chan struct{}),
		shutdownTimeout:    defaultSupervisorShutdownTimeout,
		shutdownForceGrace: defaultSupervisorForceGrace,
	}
}

func (s *RobotSupervisor) Start() {
	go s.loop()
}

func (s *RobotSupervisor) Stop() {
	if err := s.StopWithError(); err != nil {
		robotLogf("[RobotSupervisor] shutdown_incomplete err=%v\n", err)
	}
}

func (s *RobotSupervisor) StopWithError() error {
	s.once.Do(func() { close(s.stop) })
	<-s.done
	s.shutdownMu.Lock()
	defer s.shutdownMu.Unlock()
	return s.shutdownErr
}

func (s *RobotSupervisor) loop() {
	defer close(s.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			s.shutdownMu.Lock()
			s.shutdownErr = s.shutdownActors()
			s.shutdownMu.Unlock()
			return
		case now := <-ticker.C:
			s.tick(now)
		}
	}
}

func (s *RobotSupervisor) tick(now time.Time) {
	s.ledger.ReapDoneDraining()
	signals := s.manager.adaptiveSchedulerSignals()
	rc, decision := s.manager.refreshAdaptiveRobotConfig(signals)
	s.manager.updateSchedulerStatus(rc, signals, decision)
	s.sendSystemAnnouncementIfDue(now)
	if s.handleAutoGuards(now, rc, signals) {
		return
	}
	s.maintainTarget(rc)
	s.releaseBrokenLeases(now, rc)
	s.cleanupBlockedUIDs(10)
	s.recycleUnhealthyActors(now, rc)
	s.assignIdleAutoActors(rc)
	s.updateMetrics(rc, signals)
}
