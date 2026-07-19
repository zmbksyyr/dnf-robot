package message

import "encoding/json"

type MsgListUserState int

const (
	STOP  MsgListUserState = 0
	INIT  MsgListUserState = 1
	LOGIN MsgListUserState = 2
	RUN   MsgListUserState = 3
	CLEAN MsgListUserState = 4
	WRONG MsgListUserState = 5
)

type MsgListUserError int

const (
	NONE_ERROR          MsgListUserError = 0
	CONNECT_ERROR       MsgListUserError = 1
	START_RECV_ERROR    MsgListUserError = 2
	CLOSE_ERROR         MsgListUserError = 3
	LOGIN_SEND_ERROR    MsgListUserError = 4
	MAX_RECONNECT_ERROR MsgListUserError = 5
	NORMAL_DROP_ERROR   MsgListUserError = 6
	BAD_TOKEN_ERROR     MsgListUserError = 7
	BAD_CID_ERROR       MsgListUserError = 8
	BAD_UID_ERROR       MsgListUserError = 9
	REPEAT_LOGIN_ERROR  MsgListUserError = 10
	PUNISH_REASON_ERROR MsgListUserError = 11
)

type MsgListResult struct {
	ID        *int              `json:"id,omitempty"`
	IP        *string           `json:"ip,omitempty"`
	Port      *int              `json:"port,omitempty"`
	UID       *int              `json:"uid,omitempty"`
	CID       *int              `json:"cid,omitempty"`
	ConnCount *int              `json:"conncount,omitempty"`
	MaxReconn *int              `json:"maxreconn,omitempty"`
	UserState *MsgListUserState `json:"userstate,omitempty"`
	LastError *MsgListUserError `json:"lasterror,omitempty"`
	CurVill   *int              `json:"curvill,omitempty"`
	CurArea   *int              `json:"curarea,omitempty"`
	CurX      *int              `json:"curx,omitempty"`
	CurY      *int              `json:"cury,omitempty"`
	RobotType *int              `json:"robot_type,omitempty"`
	Runtime   *int              `json:"runtime,omitempty"`
}

func NewMsgListResult() *MsgListResult {
	return &MsgListResult{}
}

func (m *MsgListResult) SetId(v int) { m.ID = &v }
func (m *MsgListResult) Id() int {
	if m.ID != nil {
		return *m.ID
	}
	return 0
}
func (m *MsgListResult) HasId() bool    { return m.ID != nil }
func (m *MsgListResult) SetIp(v string) { m.IP = &v }
func (m *MsgListResult) Ip() string {
	if m.IP != nil {
		return *m.IP
	}
	return ""
}
func (m *MsgListResult) HasIp() bool   { return m.IP != nil }
func (m *MsgListResult) SetPort(v int) { m.Port = &v }
func (m *MsgListResult) GetPort() int {
	if m.Port != nil {
		return *m.Port
	}
	return 0
}
func (m *MsgListResult) HasPort() bool { return m.Port != nil }
func (m *MsgListResult) SetUid(v int)  { m.UID = &v }
func (m *MsgListResult) Uid() int {
	if m.UID != nil {
		return *m.UID
	}
	return 0
}
func (m *MsgListResult) HasUid() bool { return m.UID != nil }
func (m *MsgListResult) SetCid(v int) { m.CID = &v }
func (m *MsgListResult) Cid() int {
	if m.CID != nil {
		return *m.CID
	}
	return 0
}
func (m *MsgListResult) HasCid() bool       { return m.CID != nil }
func (m *MsgListResult) SetConncount(v int) { m.ConnCount = &v }
func (m *MsgListResult) Conncount() int {
	if m.ConnCount != nil {
		return *m.ConnCount
	}
	return 0
}
func (m *MsgListResult) HasConncount() bool { return m.ConnCount != nil }
func (m *MsgListResult) SetMaxreconn(v int) { m.MaxReconn = &v }
func (m *MsgListResult) Maxreconn() int {
	if m.MaxReconn != nil {
		return *m.MaxReconn
	}
	return 0
}
func (m *MsgListResult) HasMaxreconn() bool              { return m.MaxReconn != nil }
func (m *MsgListResult) SetUserstate(v MsgListUserState) { m.UserState = &v }
func (m *MsgListResult) Userstate() MsgListUserState {
	if m.UserState != nil {
		return *m.UserState
	}
	return 0
}
func (m *MsgListResult) HasUserstate() bool              { return m.UserState != nil }
func (m *MsgListResult) SetLasterror(v MsgListUserError) { m.LastError = &v }
func (m *MsgListResult) Lasterror() MsgListUserError {
	if m.LastError != nil {
		return *m.LastError
	}
	return 0
}
func (m *MsgListResult) HasLasterror() bool { return m.LastError != nil }
func (m *MsgListResult) SetCurvill(v int)   { m.CurVill = &v }
func (m *MsgListResult) Curvill() int {
	if m.CurVill != nil {
		return *m.CurVill
	}
	return 0
}
func (m *MsgListResult) HasCurvill() bool { return m.CurVill != nil }
func (m *MsgListResult) SetCurarea(v int) { m.CurArea = &v }
func (m *MsgListResult) Curarea() int {
	if m.CurArea != nil {
		return *m.CurArea
	}
	return 0
}
func (m *MsgListResult) HasCurarea() bool { return m.CurArea != nil }
func (m *MsgListResult) SetCurx(v int)    { m.CurX = &v }
func (m *MsgListResult) Curx() int {
	if m.CurX != nil {
		return *m.CurX
	}
	return 0
}
func (m *MsgListResult) HasCurx() bool { return m.CurX != nil }
func (m *MsgListResult) SetCury(v int) { m.CurY = &v }
func (m *MsgListResult) Cury() int {
	if m.CurY != nil {
		return *m.CurY
	}
	return 0
}
func (m *MsgListResult) HasCury() bool    { return m.CurY != nil }
func (m *MsgListResult) SetRuntime(v int) { m.Runtime = &v }
func (m *MsgListResult) GetRuntime() int {
	if m.Runtime != nil {
		return *m.Runtime
	}
	return 0
}
func (m *MsgListResult) HasRuntime() bool   { return m.Runtime != nil }
func (m *MsgListResult) SetRobotType(v int) { m.RobotType = &v }

func (m *MsgListResult) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgListResult) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgListResult) ParseFromRaw(raw *MsgListResult) error {
	if raw == nil {
		return nil
	}
	if raw.ID != nil {
		v := *raw.ID
		m.ID = &v
	}
	if raw.IP != nil {
		v := *raw.IP
		m.IP = &v
	}
	if raw.Port != nil {
		v := *raw.Port
		m.Port = &v
	}
	if raw.UID != nil {
		v := *raw.UID
		m.UID = &v
	}
	if raw.CID != nil {
		v := *raw.CID
		m.CID = &v
	}
	if raw.ConnCount != nil {
		v := *raw.ConnCount
		m.ConnCount = &v
	}
	if raw.MaxReconn != nil {
		v := *raw.MaxReconn
		m.MaxReconn = &v
	}
	if raw.UserState != nil {
		v := *raw.UserState
		m.UserState = &v
	}
	if raw.LastError != nil {
		v := *raw.LastError
		m.LastError = &v
	}
	if raw.CurVill != nil {
		v := *raw.CurVill
		m.CurVill = &v
	}
	if raw.CurArea != nil {
		v := *raw.CurArea
		m.CurArea = &v
	}
	if raw.CurX != nil {
		v := *raw.CurX
		m.CurX = &v
	}
	if raw.CurY != nil {
		v := *raw.CurY
		m.CurY = &v
	}
	if raw.RobotType != nil {
		v := *raw.RobotType
		m.RobotType = &v
	}
	if raw.Runtime != nil {
		v := *raw.Runtime
		m.Runtime = &v
	}
	return nil
}

type MsgListRequest struct {
	Key *string `json:"key,omitempty"`
}

func NewMsgListRequest() *MsgListRequest {
	return &MsgListRequest{}
}

func (m *MsgListRequest) SetKey(v string) { m.Key = &v }
func (m *MsgListRequest) GetKey() string {
	if m.Key != nil {
		return *m.Key
	}
	return ""
}
func (m *MsgListRequest) HasKey() bool { return m.Key != nil }

func (m *MsgListRequest) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgListRequest) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgListRequest) ParseFromRaw(raw *MsgListRequest) error {
	if raw == nil {
		return nil
	}
	if raw.Key != nil {
		v := *raw.Key
		m.Key = &v
	}
	return nil
}

type MsgListRespond struct {
	UserStatus []*MsgListResult `json:"userstatus,omitempty"`
}

func NewMsgListRespond() *MsgListRespond {
	return &MsgListRespond{}
}

func (m *MsgListRespond) SetUserstatus(items []*MsgListResult) { m.UserStatus = items }
func (m *MsgListRespond) AddUserstatus(item *MsgListResult) {
	m.UserStatus = append(m.UserStatus, item)
}

func (m *MsgListRespond) Userstatus(index int) *MsgListResult {
	if index < 0 || index >= len(m.UserStatus) {
		return NewMsgListResult()
	}
	return m.UserStatus[index]
}

func (m *MsgListRespond) UserstatusSize() int { return len(m.UserStatus) }
func (m *MsgListRespond) HasUserstatus() bool { return m.UserStatus != nil }

func (m *MsgListRespond) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgListRespond) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgListRespond) ParseFromRaw(raw *MsgListRespond) error {
	if raw == nil {
		return nil
	}
	if raw.UserStatus != nil {
		m.UserStatus = make([]*MsgListResult, len(raw.UserStatus))
		for i, item := range raw.UserStatus {
			cp := NewMsgListResult()
			cp.ParseFromRaw(item)
			m.UserStatus[i] = cp
		}
	}
	return nil
}
