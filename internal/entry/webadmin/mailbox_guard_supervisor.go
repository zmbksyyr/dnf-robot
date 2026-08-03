package webadmin

import (
	"errors"
	"time"

	foundationlog "robot/internal/foundation/log"
)

const mailboxGuardReconcileInterval = 5 * time.Second

func (s *Server) startMailboxGuardSupervisor() func() {
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
			case <-s.mailboxGuardWake:
				timer.Stop()
			case <-timer.C:
			}
			delay = runBackgroundSupervisorStep("MAILBOX_GUARD", mailboxGuardReconcileInterval, s.reconcileMailboxGuardOnce)
		}
	}()
	return stopBackgroundSupervisor(stop, done)
}

func (s *Server) reconcileMailboxGuardOnce() time.Duration {
	cfg, err := s.loadMailboxGuardConfig()
	if err != nil {
		foundationlog.Robotf("[MAILBOX_GUARD] config_error err=%v\n", err)
		return mailboxGuardReconcileInterval
	}
	s.reconcileMailboxGuard(cfg)
	return mailboxGuardReconcileInterval
}

func (s *Server) wakeMailboxGuardSupervisor() {
	if s == nil || s.mailboxGuardWake == nil {
		return
	}
	select {
	case s.mailboxGuardWake <- struct{}{}:
	default:
	}
}

func (s *Server) reconcileMailboxGuard(cfg mailboxGuardConfig) {
	// Party compatibility also patches df_game_r memory at startup.
	s.partyCompatMu.Lock()
	defer s.partyCompatMu.Unlock()
	status := inspectMailboxGuard(s.cfg.RobotGamePort)
	if status.State == "unavailable" {
		foundationlog.Robotf("[MAILBOX_GUARD] waiting_for_game port=%d err=%s\n", s.cfg.RobotGamePort, status.Message)
		return
	}
	if status.State == "unsupported" {
		foundationlog.Robotf("[MAILBOX_GUARD] refused pid=%d state=%s err=%s\n", status.PID, status.State, status.Message)
		return
	}
	if status.State == "error" {
		foundationlog.Robotf("[MAILBOX_GUARD] transient_inspect_failed pid=%d err=%s\n", status.PID, status.Message)
		return
	}
	if status.Enabled == cfg.Enabled {
		return
	}
	updated, err := setMailboxGuard(s.cfg.RobotGamePort, cfg.Enabled)
	if err != nil {
		if errors.Is(err, errPartyCompatUnavailable) {
			foundationlog.Robotf("[MAILBOX_GUARD] waiting_for_game port=%d err=%v\n", s.cfg.RobotGamePort, err)
			return
		}
		foundationlog.Robotf("[MAILBOX_GUARD] apply_failed enabled=%t pid=%d err=%v\n", cfg.Enabled, status.PID, err)
		return
	}
	foundationlog.Robotf("[MAILBOX_GUARD] applied enabled=%t pid=%d state=%s\n", cfg.Enabled, updated.PID, updated.State)
}
