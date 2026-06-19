package service

import (
	"time"
)

func (a *robotActor) status(now time.Time, rc robotRuntimeConfig) robotActorStatus {
	s := a.snapshot()
	status := robotActorStatus{robotActorSnapshot: s, Health: robotActorHealthHealthy}
	if s.Mode != robotActorAuto || s.UID <= 0 {
		status.Health = robotActorHealthIdle
		return status
	}
	if s.Busy {
		status.Health = robotActorHealthBusy
		status.HealthReason = s.BusyKind
		return status
	}
	if s.Failures >= rc.SchedulerBadFailures {
		status.Health = robotActorHealthUnhealthy
		status.HealthReason = "failure_count"
		status.RecycleUID = true
		return status
	}
	if s.State == robotActorOnline && !s.LastOnlineTry.IsZero() && !s.FirstFailureAt.IsZero() {
		timeout := time.Duration(rc.OnlineConfirmTimeoutMS) * time.Millisecond
		if timeout <= 0 {
			timeout = 60 * time.Second
		}
		if now.Sub(s.LastOnlineTry) > timeout {
			if a.runtime == nil {
				status.Health = robotActorHealthUnhealthy
				status.HealthReason = "online_confirm_timeout"
				status.RecycleUID = true
				return status
			}
			if st, ok := a.runtime.Status(s.UID); !ok || st.StateName != "running" || st.DisconnectReason != 0 {
				status.Health = robotActorHealthUnhealthy
				status.HealthReason = "online_confirm_timeout"
				status.RecycleUID = true
				return status
			}
		}
	}
	if s.Failures > 0 && !s.FirstFailureAt.IsZero() && now.Sub(s.FirstFailureAt) >= time.Duration(rc.SchedulerBadRecoverSec)*time.Second {
		status.Health = robotActorHealthUnhealthy
		status.HealthReason = "failure_window"
		status.RecycleUID = true
		return status
	}
	return status
}
