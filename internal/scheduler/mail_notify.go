package scheduler

import (
	"context"
	"errors"
	"time"

	robotconfig "robot/internal/capability/robotconfig"
)

var mailNotifyShutdownTimeout = 5 * time.Second

type MailNotifier interface {
	PollOnce(ctx context.Context, now time.Time) error
}

func (m *RobotManager) SetMailNotifier(notifier MailNotifier) {
	if m == nil || notifier == nil {
		return
	}
	m.autoMu.Lock()
	m.mailNotifier = notifier
	m.mailNotifyNext = time.Time{}
	m.autoMu.Unlock()
}

func (m *RobotManager) pollMailNotifications(now time.Time, rc robotconfig.RuntimeConfig) {
	if m == nil || !rc.AutoMailNotify || !m.autoActionsEnabled(rc) {
		return
	}
	interval := time.Duration(rc.SystemActorPollMS) * time.Millisecond
	if interval < time.Second {
		interval = 3 * time.Second
	}
	m.autoMu.Lock()
	notifier := m.mailNotifier
	if notifier == nil || m.mailNotifyRunning || (!m.mailNotifyNext.IsZero() && now.Before(m.mailNotifyNext)) {
		m.autoMu.Unlock()
		return
	}
	m.mailNotifyNext = now.Add(interval)
	m.mailNotifyRunning = true
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	m.mailNotifyDone = done
	m.mailNotifyCancel = cancel
	m.autoMu.Unlock()

	go func() {
		defer cancel()
		err := notifier.PollOnce(ctx, now)
		m.autoMu.Lock()
		m.mailNotifyRunning = false
		if m.mailNotifyDone == done {
			m.mailNotifyDone = nil
			m.mailNotifyCancel = nil
		}
		shouldLog := err != nil && !errors.Is(err, context.Canceled) && (m.mailNotifyLastErrorLog.IsZero() || now.Sub(m.mailNotifyLastErrorLog) >= 30*time.Second)
		if err != nil && shouldLog {
			m.mailNotifyLastErrorLog = now
		}
		m.autoMu.Unlock()
		if shouldLog {
			robotLogf("[MAIL_NOTIFY] scheduler_poll_error err=%v\n", err)
		}
		close(done)
	}()
}

func (m *RobotManager) waitMailNotifications() {
	if m == nil {
		return
	}
	m.autoMu.Lock()
	done := m.mailNotifyDone
	cancel := m.mailNotifyCancel
	m.autoMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		timer := time.NewTimer(mailNotifyShutdownTimeout)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			robotLogf("[MAIL_NOTIFY] shutdown_timeout timeout=%s\n", mailNotifyShutdownTimeout)
		}
	}
}
