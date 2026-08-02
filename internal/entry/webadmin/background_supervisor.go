package webadmin

import (
	"runtime/debug"
	"sync"
	"time"

	foundationlog "robot/internal/foundation/log"
)

func runBackgroundSupervisorStep(name string, fallback time.Duration, step func() time.Duration) (delay time.Duration) {
	delay = fallback
	defer func() {
		if rec := recover(); rec != nil {
			foundationlog.Robotf("[%s] panic err=%v\n%s", name, rec, debug.Stack())
		}
	}()
	if step == nil {
		return delay
	}
	return step()
}

func stopBackgroundSupervisor(stop chan struct{}, done <-chan struct{}) func() {
	var once sync.Once
	return func() {
		once.Do(func() { close(stop) })
		<-done
	}
}
