package dnf

type moveInternalData struct {
	ID       int
	Village  uint8
	Area     uint8
	X        uint16
	Y        uint16
	MoveType uint8
	Speed    uint16
}

type publicMsgInternalData struct {
	ID   int
	Msg  string
	Type int
}

func (t *RobotDnfTask) initKeyCall() {
	t.keyToHandle["MsgOnLine"] = t.dnfMsgOnLine
	t.keyToHandle["MsgMove"] = t.dnfMsgMove
	t.keyToHandle["MsgLogout"] = t.msgLogout
	t.keyToHandle["MsgPublicMsg"] = t.msgPublicMsg
	t.keyToHandle["MsgOnLineAsyncTaskVec"] = t.msgOnLineAsyncTaskVec
}

func (t *RobotDnfTask) dnfMsgOnLine(_ *RobotDnfTask, voVoid interface{}) bool {
	vo := voVoid.(*RobotVo)

	tmpVo := t.Find(int(vo.UID))
	if tmpVo != nil {
		if !tmpVo.CheckUserState() {
			t.DeleteByInt(int(vo.UID))
		} else {
			vo.mu.Lock()
			tasks := append([]AsyncTask(nil), vo.AfterRunAsyncTaskVec...)
			vo.mu.Unlock()
			if len(tasks) > 0 {
				tmpVo.mu.Lock()
				tmpVo.AfterRunAsyncTaskVec = tasks
				tmpVo.mu.Unlock()
				t.AddMessage("MsgOnLineAsyncTaskVec", tmpVo)
			}
			return true
		}
	}

	vo.mu.Lock()
	vo.Controller = t
	vo.IsTokenRight = false
	vo.mu.Unlock()
	return t.enqueueConnect(vo)
}

func (t *RobotDnfTask) dnfMsgMove(_ *RobotDnfTask, moveVoid interface{}) bool {
	md := moveVoid.(*moveInternalData)
	voObj := t.Find(md.ID)
	if voObj != nil {
		snap := voObj.Snapshot()
		if snap.Village != md.Village || snap.Area != md.Area {
			voObj.SetArea(md.Village, md.Area, md.X, md.Y)
		}
		voObj.SetPosition(md.X, md.Y, md.MoveType, md.Speed)
		return true
	}
	return false
}

func (t *RobotDnfTask) msgLogout(_ *RobotDnfTask, moveVoid interface{}) bool {
	uid := moveVoid.(int)
	voObj := t.Find(uid)
	if voObj != nil {
		t.DeleteByInt(uid)
		voObj.CloseOut()
	}
	return true
}

func (t *RobotDnfTask) msgPublicMsg(_ *RobotDnfTask, moveVoid interface{}) bool {
	md := moveVoid.(*publicMsgInternalData)
	voObj := t.Find(md.ID)
	if voObj != nil {
		msgType := md.Type
		if msgType == 0 {
			msgType = 3
		}
		voObj.SendPublicMessage(msgType, []byte(md.Msg))
	}
	return voObj != nil
}

func (t *RobotDnfTask) msgOnLineAsyncTaskVec(_ *RobotDnfTask, moveVoid interface{}) bool {
	vo := moveVoid.(*RobotVo)
	vo.mu.Lock()
	tasks := append([]AsyncTask(nil), vo.AfterRunAsyncTaskVec...)
	vo.AfterRunAsyncTaskVec = nil
	vo.mu.Unlock()

	for _, task := range tasks {
		switch task.Type {
		case AsyncMove:
		case AsyncDisjoint:
			vo.OpenDisjointStore(uint32(task.Cost))
		case AsyncPriStore:
			vo.mu.Lock()
			vo.PendingStoreTitle = task.Title
			vo.mu.Unlock()
			vo.CreatePrivateStore()
			vo.GetCompleteDisplay(0)
		}
	}
	return true
}
