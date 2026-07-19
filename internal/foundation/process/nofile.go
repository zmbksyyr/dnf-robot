package process

import "fmt"

const openFileHeadroom uint64 = 256

type fileLimit struct {
	soft uint64
	hard uint64
}

type fileLimitAPI interface {
	get() (fileLimit, error)
	set(fileLimit) error
}

// RequiredOpenFiles returns the minimum descriptor capacity for the configured
// robot and database connection limits.
func RequiredOpenFiles(maxOnlineRobots, dbMaxConnections int) (uint64, error) {
	if maxOnlineRobots <= 0 {
		return 0, fmt.Errorf("max_online_robots must be positive: %d", maxOnlineRobots)
	}
	if dbMaxConnections <= 0 {
		return 0, fmt.Errorf("db_max_size must be positive: %d", dbMaxConnections)
	}

	robots := uint64(maxOnlineRobots)
	database := uint64(dbMaxConnections)
	maxUint64 := ^uint64(0)
	if database > maxUint64-openFileHeadroom || robots > (maxUint64-openFileHeadroom-database)/2 {
		return 0, fmt.Errorf("open file requirement overflows: max_online_robots=%d db_max_size=%d", maxOnlineRobots, dbMaxConnections)
	}
	return 2*robots + database + openFileHeadroom, nil
}

func ensureOpenFileLimit(api fileLimitAPI, maxOnlineRobots, dbMaxConnections int) error {
	required, err := RequiredOpenFiles(maxOnlineRobots, dbMaxConnections)
	if err != nil {
		return err
	}

	current, err := api.get()
	if err != nil {
		return fmt.Errorf("read RLIMIT_NOFILE: %w", err)
	}
	if current.soft >= required {
		return nil
	}

	desired := current
	desired.soft = required
	if desired.hard < required {
		desired.hard = required
	}
	setErr := api.set(desired)

	actual, getErr := api.get()
	if getErr != nil {
		if setErr != nil {
			return fmt.Errorf("raise RLIMIT_NOFILE to %d and verify it: raise: %v; verify: %w", required, setErr, getErr)
		}
		return fmt.Errorf("verify RLIMIT_NOFILE after raising it to %d: %w", required, getErr)
	}
	if actual.soft >= required {
		return nil
	}

	detail := fmt.Sprintf(
		"RLIMIT_NOFILE is insufficient: required=%d (2*max_online_robots=%d + db_max_size=%d + headroom=%d), soft=%d hard=%d",
		required,
		2*uint64(maxOnlineRobots),
		dbMaxConnections,
		openFileHeadroom,
		actual.soft,
		actual.hard,
	)
	if setErr != nil {
		return fmt.Errorf("%s: unable to raise limit: %w", detail, setErr)
	}
	return fmt.Errorf("%s: limit remained below the requested value", detail)
}
