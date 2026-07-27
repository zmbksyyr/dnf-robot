package scheduler

import (
	"errors"
	"testing"

	"robot/internal/foundation/config"
)

func TestInvalidateCharacterCacheUsesInjectedBoundary(t *testing.T) {
	want := errors.New("cache unavailable")
	manager := NewRobotManager(nil, &config.SysConfig{}, nil)
	calledUID := 0
	manager.characterCacheInvalidate = func(uid int) error {
		calledUID = uid
		return want
	}

	err := manager.invalidateCharacterCache(17000001)
	if !errors.Is(err, want) || calledUID != 17000001 {
		t.Fatalf("invalidate error=%v uid=%d, want injected error and uid", err, calledUID)
	}
}
