package webadmin

import (
	"errors"
	"strings"
	"time"

	foundationlog "robot/internal/foundation/log"
)

const (
	partyCompatSuccessInterval      = 30 * time.Second
	partyCompatInitialRetry         = 5 * time.Second
	partyCompatMaxRetry             = 60 * time.Second
	partyCompatDisableAfterFailures = 10
	partyCompatDisableAfter         = 5 * time.Minute
)

func (s *Server) startPartyCompatSupervisor() func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		delay := time.Duration(0)
		for {
			timer := time.NewTimer(delay)
			select {
			case <-stop:
				timer.Stop()
				return
			case <-s.partyCompatWake:
				timer.Stop()
			case <-timer.C:
			}
			delay = s.reconcilePartyCompat()
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}

func (s *Server) wakePartyCompatSupervisor() {
	if s == nil || s.partyCompatWake == nil {
		return
	}
	select {
	case s.partyCompatWake <- struct{}{}:
	default:
	}
}

func (s *Server) reconcilePartyCompat() time.Duration {
	s.partyCompatMu.Lock()
	defer s.partyCompatMu.Unlock()

	cfg, err := s.loadPartyCompatConfig()
	if err != nil {
		foundationlog.Robotf("[PARTY_COMPAT_SUPERVISOR] config_error err=%v\n", err)
		return partyCompatMaxRetry
	}
	status := inspectPartyCompat(s.cfg.RobotGamePort, cfg)
	if !cfg.Enabled {
		if status.Enabled || status.orphanedCave {
			if _, err := setPartyCompat(s.cfg.RobotGamePort, cfg, false); err != nil {
				s.recordPartyCompatFailureLocked(err)
				foundationlog.Robotf("[PARTY_COMPAT_SUPERVISOR] disable_failed pid=%d err=%v\n", status.PID, err)
				return partyCompatRetryDelay(s.partyCompatFailures)
			}
			foundationlog.Robotf("[PARTY_COMPAT_SUPERVISOR] disabled pid=%d\n", status.PID)
		}
		s.resetPartyCompatFailuresLocked()
		return partyCompatSuccessInterval
	}

	if status.Enabled && status.AccountStart == cfg.AccountStart && status.AccountEnd == cfg.AccountEnd {
		s.resetPartyCompatFailuresLocked()
		return partyCompatSuccessInterval
	}
	if status.processUnavailable {
		delay := s.schedulePartyCompatUnavailableRetryLocked(status.Message, time.Now())
		foundationlog.Robotf("[PARTY_COMPAT_SUPERVISOR] waiting_for_game port=%d retry=%s err=%s\n", s.cfg.RobotGamePort, delay, status.Message)
		return delay
	}
	if _, err := setPartyCompat(s.cfg.RobotGamePort, cfg, true); err != nil {
		if errors.Is(err, errPartyCompatUnavailable) {
			delay := s.schedulePartyCompatUnavailableRetryLocked(err.Error(), time.Now())
			foundationlog.Robotf("[PARTY_COMPAT_SUPERVISOR] waiting_for_game port=%d retry=%s err=%v\n", s.cfg.RobotGamePort, delay, err)
			return delay
		}
		s.recordPartyCompatFailureLocked(err)
		foundationlog.Robotf("[PARTY_COMPAT_SUPERVISOR] enable_failed failures=%d port=%d err=%v\n", s.partyCompatFailures, s.cfg.RobotGamePort, err)
		if s.partyCompatShouldDisableLocked(time.Now()) {
			cfg.Enabled = false
			if saveErr := s.savePartyCompatConfig(cfg); saveErr != nil {
				foundationlog.Robotf("[PARTY_COMPAT_SUPERVISOR] auto_off_save_failed err=%v\n", saveErr)
			} else {
				foundationlog.Robotf("[PARTY_COMPAT_SUPERVISOR] auto_off failures=%d err=%v\n", s.partyCompatFailures, err)
			}
			s.resetPartyCompatFailuresLocked()
			return partyCompatSuccessInterval
		}
		delay := partyCompatRetryDelay(s.partyCompatFailures)
		s.partyCompatNextRetry = time.Now().Add(delay)
		return delay
	}
	foundationlog.Robotf("[PARTY_COMPAT_SUPERVISOR] enabled port=%d range=%d..%d\n", s.cfg.RobotGamePort, cfg.AccountStart, cfg.AccountEnd)
	s.resetPartyCompatFailuresLocked()
	return partyCompatSuccessInterval
}

func (s *Server) schedulePartyCompatUnavailableRetryLocked(message string, now time.Time) time.Duration {
	delay := partyCompatInitialRetry
	s.partyCompatFailures = 0
	s.partyCompatFirstFailure = time.Time{}
	s.partyCompatLastError = partyCompatWaitingMessage(message)
	s.partyCompatNextRetry = now.Add(delay)
	return delay
}

func partyCompatWaitingMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return "waiting for df_game_r"
	}
	if strings.HasPrefix(message, "waiting for df_game_r:") {
		return message
	}
	return "waiting for df_game_r: " + message
}

func (s *Server) recordPartyCompatFailureLocked(err error) {
	now := time.Now()
	if s.partyCompatFailures == 0 {
		s.partyCompatFirstFailure = now
	}
	s.partyCompatFailures++
	s.partyCompatLastError = err.Error()
}

func (s *Server) resetPartyCompatFailuresLocked() {
	s.partyCompatFailures = 0
	s.partyCompatFirstFailure = time.Time{}
	s.partyCompatNextRetry = time.Time{}
	s.partyCompatLastError = ""
}

func (s *Server) partyCompatShouldDisableLocked(now time.Time) bool {
	if s.partyCompatFailures < partyCompatDisableAfterFailures || s.partyCompatFirstFailure.IsZero() {
		return false
	}
	return now.Sub(s.partyCompatFirstFailure) >= partyCompatDisableAfter
}

func partyCompatRetryDelay(failures int) time.Duration {
	if failures <= 0 {
		return partyCompatInitialRetry
	}
	delay := partyCompatInitialRetry
	for i := 1; i < failures; i++ {
		delay *= 2
		if delay >= partyCompatMaxRetry {
			return partyCompatMaxRetry
		}
	}
	return delay
}
