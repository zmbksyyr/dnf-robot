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
	cfg, err := s.loadMailboxGuardConfig()
	if err != nil {
		foundationlog.Robotf("[MAILBOX_GUARD] config_error err=%v\n", err)
		close(done)
		return func() {
			close(stop)
			<-done
		}
	}
	go func(startupConfig mailboxGuardConfig) {
		defer close(done)
		delay := time.Duration(0)
		for {
			timer := time.NewTimer(delay)
			select {
			case <-stop:
				timer.Stop()
				return
			case <-timer.C:
			}
			s.reconcileMailboxGuard(startupConfig)
			delay = mailboxGuardReconcileInterval
		}
	}(cfg)
	return func() {
		close(stop)
		<-done
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
	if status.State == "unsupported" || status.State == "error" {
		foundationlog.Robotf("[MAILBOX_GUARD] refused pid=%d state=%s err=%s\n", status.PID, status.State, status.Message)
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
