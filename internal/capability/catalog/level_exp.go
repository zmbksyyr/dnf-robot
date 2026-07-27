package catalog

import (
	"fmt"

	"robot/internal/foundation/lockhub"
)

var levelExpState struct {
	lockhub.RWLocker
	values []int
}

// SetLevelMinExpTable installs the level curve exported from the active PVF.
// The table is indexed by character level; level 1 must be present.
func SetLevelMinExpTable(values []int) error {
	if len(values) < 2 {
		return fmt.Errorf("level experience table is too short: %d", len(values))
	}
	if values[0] != 0 || values[1] != 0 {
		return fmt.Errorf("level experience table must start with level 1 at zero")
	}
	for level := 2; level < len(values); level++ {
		if values[level] < values[level-1] {
			return fmt.Errorf("level experience decreases at level %d", level)
		}
	}
	copyValues := append([]int(nil), values...)
	levelExpState.Lock()
	levelExpState.values = copyValues
	levelExpState.Unlock()
	return nil
}

// LevelMinExp returns the minimum accumulated experience for a character level
// from the active PVF export.
func LevelMinExp(level int) (int, bool) {
	levelExpState.RLock()
	defer levelExpState.RUnlock()
	if level < 1 || level >= len(levelExpState.values) {
		return 0, false
	}
	return levelExpState.values[level], true
}
