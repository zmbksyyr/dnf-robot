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
	t.keyToHandle["MsgReconnect"] = t.msgReconnect
	t.keyToHandle["MsgMove"] = t.dnfMsgMove
	t.keyToHandle["MsgLogout"] = t.msgLogout
	t.keyToHandle["MsgPublicMsg"] = t.msgPublicMsg
}

func (t *RobotDnfTask) dnfMsgOnLine(_ *RobotDnfTask, voVoid interface{}) bool {
	vo := voVoid.(*RobotVo)

	tmpVo := t.Find(int(vo.UID))
	if tmpVo != nil && tmpVo.CheckUserState() {
		return true
	}

	if !vo.prepareConnect(t) || !t.replaceCurrent(vo.UID, tmpVo, vo) {
		vo.CloseOut()
		return false
	}
	if t.enqueueConnect(vo) {
		return true
	}
	vo.CloseOut()
	t.DeleteIf(vo.UID, vo)
	return false
}

func (t *RobotDnfTask) msgReconnect(_ *RobotDnfTask, voVoid interface{}) bool {
	vo, ok := voVoid.(*RobotVo)
	if !ok || vo == nil || !t.isCurrent(vo.UID, vo) || !vo.readyToConnect() {
		return false
	}
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
		voObj.CloseOut()
		t.DeleteIf(uint32(uid), voObj)
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
