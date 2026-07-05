package robotaction

import (
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	robottemplate "robot/internal/capability/robottemplate"
	"robot/internal/foundation/mathx"
	"strings"
	"time"
)

type ShoutService struct {
	Env ShoutEnv
}

type ShoutEnv interface {
	AppendShout(uid int, channel string, typ int, msg string) error
	AppendShoutDetail(uid int, channel string, typ int, msg, source string) error
	Config() robotconfig.RuntimeConfig
	LookupRobotName(uid int) string
	RandBetween(min, max int) int
	RandIntn(n int) int
	RuntimeStatusMap() map[int]robotcap.RuntimeStatus
	SelectRobots(req robotcap.CommandRequest) ([]robotcap.Info, error)
	SendLocalShout(source string, uid int, msg string, msgType int) error
	SendWorldShout(msg, name string, senderID uint16) error
	Templates() robottemplate.ShoutTemplates
}

func (s ShoutService) ShoutOne(req robotcap.CommandRequest, world bool) (robotcap.CommandResult, error) {
	env := s.Env
	robots, err := env.SelectRobots(req)
	if err != nil {
		return robotcap.CommandResult{}, err
	}
	tpl := env.Templates()
	rc := env.Config()
	channel := "local"
	if world {
		channel = "world"
	}
	result := robotcap.NewCommandResult(len(robots))
	for _, robot := range robots {
		msg := robottemplate.SafeShoutMessage(tpl.Messages[env.RandIntn(len(tpl.Messages))])
		msgType, _, _ := robottemplate.PrepareShout(msg, world)
		if !rc.ShoutSendEnabled {
			_ = env.AppendShout(robot.UID, channel, msgType, msg)
			result.Accepted++
			result.Confirmed++
			result.Robots = append(result.Robots, robotcap.ActionResult{UID: robot.UID, CID: robot.CID, OK: true, State: robotcap.ActionStateSent, Message: msg})
		} else if err := s.Send("manager", robot.UID, robot.Name, tpl, msg, world, rc); err != nil {
			result.Failed++
			result.Robots = append(result.Robots, robotcap.ActionResult{UID: robot.UID, CID: robot.CID, OK: false, State: robotcap.ActionStateFailed, Message: err.Error()})
		} else {
			result.Accepted++
			result.Robots = append(result.Robots, robotcap.ActionResult{UID: robot.UID, CID: robot.CID, OK: false, State: robotcap.ActionStateAccepted, Message: msg})
		}
	}
	time.Sleep(500 * time.Millisecond)
	status := env.RuntimeStatusMap()
	for i := range result.Robots {
		if !strings.EqualFold(result.Robots[i].State, robotcap.ActionStateAccepted) {
			continue
		}
		if st, ok := status[result.Robots[i].UID]; ok && robotcap.ActiveRuntimeStatus(st) {
			result.Robots[i].OK = true
			result.Robots[i].State = robotcap.ActionStateSent
			result.Confirmed++
		} else {
			result.Robots[i].State = robotcap.ActionStateNotConfirmed
			result.Failed++
		}
	}
	return result, nil
}

func (s ShoutService) AutoShout(uid int, msg string, world bool) {
	env := s.Env
	msg = robottemplate.SafeShoutMessage(msg)
	rc := env.Config()
	msgType, channel, _ := robottemplate.PrepareShout(msg, world)
	if !rc.ShoutSendEnabled {
		_ = env.AppendShout(uid, channel, msgType, msg)
		return
	}
	_ = s.Send("auto", uid, env.LookupRobotName(uid), env.Templates(), msg, world, rc)
}

func (s ShoutService) Send(source string, uid int, senderName string, _ robottemplate.ShoutTemplates, msg string, world bool, rc robotconfig.RuntimeConfig) error {
	msg = robottemplate.SafeShoutMessage(msg)
	msgType, channel, outMsg := robottemplate.PrepareShout(msg, world)
	if rc.ShoutDelayMS > 0 {
		time.Sleep(time.Duration(s.Env.RandBetween(100, mathx.MaxInt(100, rc.ShoutDelayMS/2))) * time.Millisecond)
	}
	if world {
		if err := s.Env.SendWorldShout(outMsg, senderName, uint16(uid)); err != nil {
			return err
		}
		_ = s.Env.AppendShoutDetail(uid, channel, msgType, msg, source)
		return nil
	}
	if err := s.Env.SendLocalShout(source, uid, outMsg, msgType); err != nil {
		return err
	}
	_ = s.Env.AppendShoutDetail(uid, channel, msgType, msg, source)
	return nil
}
