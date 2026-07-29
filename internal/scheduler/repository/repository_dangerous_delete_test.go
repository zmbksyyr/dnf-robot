package repository

import (
	robotcap "robot/internal/capability/robot"
	"strings"
	"testing"
)

func TestDangerousDeletePlanRejectsInvalidTargetsBeforeDatabaseAccess(t *testing.T) {
	var repo SQLRepository
	tests := []struct {
		name string
		req  robotcap.DangerousDeleteRequest
		want string
	}{
		{name: "mode", req: robotcap.DangerousDeleteRequest{Mode: "all"}, want: "invalid dangerous delete mode"},
		{name: "cid", req: robotcap.DangerousDeleteRequest{Mode: robotcap.DangerousDeleteModeCID}, want: "cid must be positive"},
		{name: "uid", req: robotcap.DangerousDeleteRequest{Mode: robotcap.DangerousDeleteModeUID}, want: "uid must be positive"},
		{name: "range order", req: robotcap.DangerousDeleteRequest{Mode: robotcap.DangerousDeleteModeRange, MinUID: 20, MaxUID: 10}, want: "invalid uid range"},
		{name: "range width", req: robotcap.DangerousDeleteRequest{Mode: robotcap.DangerousDeleteModeRange, MinUID: 1, MaxUID: maxDangerousUIDRange + 1}, want: "exceeds maximum"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := repo.DangerousDeletePlan(tt.req)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err=%v want containing %q", err, tt.want)
			}
		})
	}
}
