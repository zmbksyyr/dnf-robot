package service

import (
	"math/rand"
	"strings"
)

func (m *RobotManager) randIntn(n int) int {
	if n <= 0 {
		return 0
	}
	m.randMu.Lock()
	defer m.randMu.Unlock()
	return m.rand.Intn(n)
}

func (m *RobotManager) randBetween(min, max int) int {
	if max < min {
		min, max = max, min
	}
	return min + m.randIntn(max-min+1)
}

func (m *RobotManager) randomFrom(vals []int) int {
	if len(vals) == 0 {
		return 0
	}
	return vals[m.randIntn(len(vals))]
}

func (m *RobotManager) randomString(vals []string, fallback string) string {
	if len(vals) == 0 {
		return fallback
	}
	return vals[m.randIntn(len(vals))]
}

func (m *RobotManager) randShuffle(n int, swap func(i, j int)) {
	if n <= 1 {
		return
	}
	m.randMu.Lock()
	defer m.randMu.Unlock()
	m.rand.Shuffle(n, swap)
}

func randomBetween(r *rand.Rand, min, max int) int {
	if max < min {
		min, max = max, min
	}
	return min + r.Intn(max-min+1)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func robotUIDs(robots []RobotInfo) []int {
	out := make([]int, 0, len(robots))
	for _, r := range robots {
		out = append(out, r.UID)
	}
	return out
}

func newCommandResult(requested int) RobotCommandResult {
	return RobotCommandResult{Requested: requested, Robots: make([]RobotActionResult, 0, requested)}
}

func sqlPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", n), ",")
}
