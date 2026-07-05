package robotaction

import (
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/foundation/mathx"
	"robot/internal/shared"
	"time"
)

type MoveService struct {
	Env MoveEnv
}

type MoveEnv interface {
	DispatchMoveStep(info robotcap.Info, targetX, targetY, step, steps, speed int, rc robotconfig.RuntimeConfig) error
	LoadMapCatalog() []shared.MapCatalogItem
	RandBetween(min, max int) int
	RuntimeStatusMap() map[int]robotcap.RuntimeStatus
	SelectRobots(req robotcap.CommandRequest) ([]robotcap.Info, error)
}

type FollowTarget struct {
	UID     int
	Village int
	Area    int
	X       int
	Y       int
}

func (s MoveService) Move(req robotcap.CommandRequest, rc robotconfig.RuntimeConfig) (robotcap.CommandResult, error) {
	env := s.Env
	robots, err := env.SelectRobots(req)
	if err != nil {
		return robotcap.CommandResult{}, err
	}
	maps := env.LoadMapCatalog()
	status := env.RuntimeStatusMap()
	result := robotcap.NewCommandResult(len(robots))
	type movePlan struct {
		robot  robotcap.Info
		steps  int
		target [2]int
		speeds []int
	}
	var plans []movePlan
	for _, robot := range robots {
		if st, ok := status[robot.UID]; ok && robotcap.ActiveRuntimeStatus(st) {
			robot.Village, robot.Area, robot.X, robot.Y = st.Village, st.Area, st.X, st.Y
			if st.RobotType == 2 || st.RobotType == 3 {
				result.Robots = append(result.Robots, robotcap.ActionResult{UID: robot.UID, CID: robot.CID, OK: true, State: robotcap.ActionStateStore, Message: "skip moving store robot"})
				continue
			}
		}
		targetX, targetY := s.randomTarget(robot, rc, maps)
		steps := env.RandBetween(mathx.MaxInt(2, rc.MoveSteps-1), mathx.MinInt(8, rc.MoveSteps+2))
		speeds := make([]int, steps)
		for i := range speeds {
			speeds[i] = env.RandBetween(rc.MoveSpeedMin, rc.MoveSpeedMax)
		}
		plans = append(plans, movePlan{robot: robot, steps: steps, target: [2]int{targetX, targetY}, speeds: speeds})
		result.Accepted++
		result.Robots = append(result.Robots, robotcap.ActionResult{UID: robot.UID, CID: robot.CID, OK: false, State: robotcap.ActionStateAccepted})
	}
	maxSteps := 0
	for _, plan := range plans {
		maxSteps = mathx.MaxInt(maxSteps, plan.steps)
	}
	for step := 1; step <= maxSteps; step++ {
		for _, plan := range plans {
			if step > plan.steps {
				continue
			}
			if err := env.DispatchMoveStep(plan.robot, plan.target[0], plan.target[1], step, plan.steps, plan.speeds[step-1], rc); err != nil {
				result.Failed++
			}
		}
		if step < maxSteps {
			delay := rc.MoveStepDelayMS
			if delay > 0 {
				delay = env.RandBetween(mathx.MaxInt(300, delay/2), mathx.MaxInt(300, delay*3/2))
				time.Sleep(time.Duration(delay) * time.Millisecond)
			}
		}
	}
	waitMS := rc.MoveStepDelayMS*rc.MoveSteps + 500
	if waitMS < 500 {
		waitMS = 500
	}
	time.Sleep(time.Duration(waitMS) * time.Millisecond)
	status = env.RuntimeStatusMap()
	for i := range result.Robots {
		if st, ok := status[result.Robots[i].UID]; ok && robotcap.ActiveRuntimeStatus(st) {
			result.Robots[i].OK = true
			result.Robots[i].State = st.StateName
			result.Confirmed++
		} else if result.Robots[i].State == robotcap.ActionStateAccepted {
			result.Robots[i].State = robotcap.ActionStatePending
			result.Robots[i].Message = "move not confirmed by runtime state"
			result.Failed++
		}
	}
	return result, nil
}

func (s MoveService) AutoMove(info robotcap.Info, rc robotconfig.RuntimeConfig, maps []shared.MapCatalogItem, follow *FollowTarget) {
	targetX, targetY := s.randomTarget(info, rc, maps)
	if follow != nil {
		info.Village = follow.Village
		info.Area = follow.Area
		targetX, targetY = s.followTarget(info, *follow, rc, maps)
	}
	steps := s.Env.RandBetween(mathx.MaxInt(2, rc.MoveSteps-1), mathx.MinInt(8, rc.MoveSteps+2))
	for step := 1; step <= steps; step++ {
		if st, ok := s.Env.RuntimeStatusMap()[info.UID]; ok && (st.RobotType == 2 || st.RobotType == 3 || st.StateName != "running") {
			return
		}
		speed := s.Env.RandBetween(rc.MoveSpeedMin, rc.MoveSpeedMax)
		_ = s.Env.DispatchMoveStep(info, targetX, targetY, step, steps, speed, rc)
		if step < steps && rc.MoveStepDelayMS > 0 {
			delay := s.Env.RandBetween(mathx.MaxInt(300, rc.MoveStepDelayMS/2), mathx.MaxInt(300, rc.MoveStepDelayMS*3/2))
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
	}
}

func (s MoveService) followTarget(info robotcap.Info, target FollowTarget, rc robotconfig.RuntimeConfig, maps []shared.MapCatalogItem) (int, int) {
	xMin, xMax := target.X-rc.FollowRadiusX, target.X+rc.FollowRadiusX
	yMin, yMax := target.Y-rc.FollowRadiusY, target.Y+rc.FollowRadiusY
	if rc.FollowRadiusX <= 0 {
		xMin, xMax = target.X-120, target.X+120
	}
	if rc.FollowRadiusY <= 0 {
		yMin, yMax = target.Y-30, target.Y+30
	}
	for _, mp := range maps {
		if mp.Use && mp.Village == target.Village && mp.Area == target.Area {
			xMin = mathx.MaxInt(mp.XMin, xMin)
			xMax = mathx.MinInt(mp.XMax, xMax)
			yMin = mathx.MaxInt(mp.YMin, yMin)
			yMax = mathx.MinInt(mp.YMax, yMax)
			break
		}
	}
	if xMax <= xMin || yMax <= yMin {
		return s.randomTarget(info, rc, maps)
	}
	return s.Env.RandBetween(xMin, xMax), s.Env.RandBetween(yMin, yMax)
}

func (s MoveService) randomTarget(info robotcap.Info, rc robotconfig.RuntimeConfig, maps []shared.MapCatalogItem) (int, int) {
	xMin, xMax := rc.SpawnXMin, rc.SpawnXMax
	yMin, yMax := rc.SpawnYMin, rc.SpawnYMax
	if !rc.SpawnFixed {
		for _, mp := range maps {
			if mp.Village == info.Village && mp.Area == info.Area {
				xMin, xMax = mp.XMin, mp.XMax
				yMin, yMax = mp.YMin, mp.YMax
				break
			}
		}
	}
	if xMax <= xMin {
		xMin, xMax = mathx.MaxInt(0, info.X-120), info.X+120
	}
	if yMax <= yMin {
		yMin, yMax = mathx.MaxInt(0, info.Y-40), info.Y+40
	}
	for i := 0; i < 12; i++ {
		x := s.Env.RandBetween(xMin, xMax)
		y := s.Env.RandBetween(yMin, yMax)
		if mathx.AbsInt(x-info.X) >= 40 || mathx.AbsInt(y-info.Y) >= 15 {
			return x, y
		}
	}
	return s.Env.RandBetween(xMin, xMax), s.Env.RandBetween(yMin, yMax)
}
