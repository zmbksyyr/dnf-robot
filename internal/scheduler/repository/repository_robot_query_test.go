package repository

import (
	"strings"
	"testing"
)

func TestRobotLocationsQueryOnlyCountsNormalRoles(t *testing.T) {
	if !strings.Contains(robotLocationsQuery, "WHERE CAST(d.function_type AS SIGNED)=0") {
		t.Fatalf("robot locations query includes active store roles: %s", robotLocationsQuery)
	}
}
