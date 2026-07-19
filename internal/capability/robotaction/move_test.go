package robotaction

import (
	"errors"
	"testing"

	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/shared"
)

type capturedMoveStep struct {
	info          robotcap.Info
	targetVillage int
	targetArea    int
	targetX       int
	targetY       int
}

type captureMoveEnv struct {
	steps  []capturedMoveStep
	failAt int
}

func (e *captureMoveEnv) DispatchMoveStep(info robotcap.Info, targetVillage, targetArea, targetX, targetY, _, _, _ int, _ robotconfig.RuntimeConfig) error {
	e.steps = append(e.steps, capturedMoveStep{
		info:          info,
		targetVillage: targetVillage,
		targetArea:    targetArea,
		targetX:       targetX,
		targetY:       targetY,
	})
	if e.failAt > 0 && len(e.steps) == e.failAt {
		return errors.New("move send failed")
	}
	return nil
}

func (*captureMoveEnv) LoadMapCatalog() []shared.MapCatalogItem { return nil }
func (*captureMoveEnv) RandBetween(min, _ int) int              { return min }
func (*captureMoveEnv) RuntimeStatus(int) (robotcap.RuntimeStatus, bool) {
	return robotcap.RuntimeStatus{}, false
}
func (*captureMoveEnv) RuntimeStatusMap() map[int]robotcap.RuntimeStatus { return nil }
func (*captureMoveEnv) SelectRobots(robotcap.CommandRequest) ([]robotcap.Info, error) {
	return nil, nil
}

func TestAutoMovePreservesSourceWhenFollowingAcrossAreas(t *testing.T) {
	env := &captureMoveEnv{}
	service := MoveService{Env: env}
	source := robotcap.Info{UID: 101, CID: 201, Village: 1, Area: 2, X: 100, Y: 50}
	target := FollowTarget{UID: 999, Village: 3, Area: 4, X: 500, Y: 200}
	rc := robotconfig.RuntimeConfig{
		MoveSteps:     2,
		MoveSpeedMin:  100,
		MoveSpeedMax:  100,
		FollowRadiusX: 20,
		FollowRadiusY: 10,
	}
	maps := []shared.MapCatalogItem{{Village: 3, Area: 4, XMin: 400, XMax: 600, YMin: 150, YMax: 250, Use: true}}

	if err := service.AutoMove(source, rc, maps, &target); err != nil {
		t.Fatal(err)
	}

	if len(env.steps) != 2 {
		t.Fatalf("move steps got %d want 2", len(env.steps))
	}
	for _, step := range env.steps {
		if step.info.Village != source.Village || step.info.Area != source.Area || step.info.X != source.X || step.info.Y != source.Y {
			t.Fatalf("source position was mutated: %+v", step.info)
		}
		if step.targetVillage != target.Village || step.targetArea != target.Area {
			t.Fatalf("target area got %d/%d want %d/%d", step.targetVillage, step.targetArea, target.Village, target.Area)
		}
	}
}

func TestAutoMoveStopsAfterDispatchFailure(t *testing.T) {
	env := &captureMoveEnv{failAt: 2}
	service := MoveService{Env: env}
	rc := robotconfig.RuntimeConfig{MoveSteps: 4, MoveSpeedMin: 100, MoveSpeedMax: 100}
	err := service.AutoMove(robotcap.Info{UID: 101, X: 10, Y: 10}, rc, nil, nil)
	if err == nil || err.Error() != "move send failed" {
		t.Fatalf("AutoMove error = %v", err)
	}
	if len(env.steps) != 2 {
		t.Fatalf("move steps after failure = %d, want 2", len(env.steps))
	}
}
