package message

import (
	"encoding/json"
)

// ---- base.go ----

// ---- errormsg.go ----
type MsgErrorType int

const (
	NO_ERROR         MsgErrorType = 0
	MSG_ERROR_MSG    MsgErrorType = 1
	MSG_TYPE_ERROR   MsgErrorType = 2
	MSG_NOT_OPEN     MsgErrorType = 3
	MSG_FORMAT_ERROR MsgErrorType = 4
	KEY_BAD          MsgErrorType = 5
)

type MsgErrorRespond struct {
	Type *MsgErrorType `json:"type,omitempty"`
}

func NewMsgErrorRespond() *MsgErrorRespond {
	return &MsgErrorRespond{}
}

func (m *MsgErrorRespond) SetType(v MsgErrorType) { m.Type = &v }
func (m *MsgErrorRespond) TypeVal() MsgErrorType {
	if m.Type != nil {
		return *m.Type
	}
	return 0
}
func (m *MsgErrorRespond) HasType() bool { return m.Type != nil }

func (m *MsgErrorRespond) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgErrorRespond) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgErrorRespond) ParseFromRaw(raw *MsgErrorRespond) error {
	if raw == nil {
		return nil
	}
	if raw.Type != nil {
		v := *raw.Type
		m.Type = &v
	}
	return nil
}

// ---- list.go ----
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

// ---- logout.go ----
type MsgLogoutStatus int

const (
	LOGOUT_NO_ERROR MsgLogoutStatus = 0
	LOGOUT_ID_WRONG MsgLogoutStatus = -1
)

type MsgLogoutUserInfo struct {
	ID *int `json:"id,omitempty"`
}

func NewMsgLogoutUserInfo() *MsgLogoutUserInfo {
	return &MsgLogoutUserInfo{}
}

func (m *MsgLogoutUserInfo) SetId(v int) { m.ID = &v }
func (m *MsgLogoutUserInfo) Id() int {
	if m.ID != nil {
		return *m.ID
	}
	return 0
}
func (m *MsgLogoutUserInfo) HasId() bool { return m.ID != nil }

func (m *MsgLogoutUserInfo) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgLogoutUserInfo) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgLogoutUserInfo) ParseFromRaw(raw *MsgLogoutUserInfo) error {
	if raw == nil {
		return nil
	}
	if raw.ID != nil {
		v := *raw.ID
		m.ID = &v
	}
	return nil
}

type MsgLogoutResult struct {
	ID     *int             `json:"id,omitempty"`
	Status *MsgLogoutStatus `json:"status,omitempty"`
}

func NewMsgLogoutResult() *MsgLogoutResult {
	return &MsgLogoutResult{}
}

func (m *MsgLogoutResult) SetId(v int) { m.ID = &v }
func (m *MsgLogoutResult) Id() int {
	if m.ID != nil {
		return *m.ID
	}
	return 0
}
func (m *MsgLogoutResult) HasId() bool                 { return m.ID != nil }
func (m *MsgLogoutResult) SetStatus(v MsgLogoutStatus) { m.Status = &v }
func (m *MsgLogoutResult) GetStatus() MsgLogoutStatus {
	if m.Status != nil {
		return *m.Status
	}
	return 0
}
func (m *MsgLogoutResult) HasStatus() bool { return m.Status != nil }

func (m *MsgLogoutResult) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgLogoutResult) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgLogoutResult) ParseFromRaw(raw *MsgLogoutResult) error {
	if raw == nil {
		return nil
	}
	if raw.ID != nil {
		v := *raw.ID
		m.ID = &v
	}
	if raw.Status != nil {
		v := *raw.Status
		m.Status = &v
	}
	return nil
}

type MsgLogoutRequest struct {
	Key       *string              `json:"key,omitempty"`
	UserInfos []*MsgLogoutUserInfo `json:"userinfos,omitempty"`
}

func NewMsgLogoutRequest() *MsgLogoutRequest {
	return &MsgLogoutRequest{}
}

func (m *MsgLogoutRequest) SetKey(v string) { m.Key = &v }
func (m *MsgLogoutRequest) GetKey() string {
	if m.Key != nil {
		return *m.Key
	}
	return ""
}
func (m *MsgLogoutRequest) HasKey() bool { return m.Key != nil }

func (m *MsgLogoutRequest) SetUserinfos(items []*MsgLogoutUserInfo) { m.UserInfos = items }
func (m *MsgLogoutRequest) AddUserinfos(item *MsgLogoutUserInfo) {
	m.UserInfos = append(m.UserInfos, item)
}

func (m *MsgLogoutRequest) Userinfos(index int) *MsgLogoutUserInfo {
	if index < 0 || index >= len(m.UserInfos) {
		return NewMsgLogoutUserInfo()
	}
	return m.UserInfos[index]
}

func (m *MsgLogoutRequest) UserinfosSize() int { return len(m.UserInfos) }
func (m *MsgLogoutRequest) HasUserinfos() bool { return m.UserInfos != nil }

func (m *MsgLogoutRequest) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgLogoutRequest) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgLogoutRequest) ParseFromRaw(raw *MsgLogoutRequest) error {
	if raw == nil {
		return nil
	}
	if raw.Key != nil {
		v := *raw.Key
		m.Key = &v
	}
	if raw.UserInfos != nil {
		m.UserInfos = make([]*MsgLogoutUserInfo, len(raw.UserInfos))
		for i, item := range raw.UserInfos {
			cp := NewMsgLogoutUserInfo()
			cp.ParseFromRaw(item)
			m.UserInfos[i] = cp
		}
	}
	return nil
}

type MsgLogoutRespond struct {
	UserStatus []*MsgLogoutResult `json:"userstatus,omitempty"`
}

func NewMsgLogoutRespond() *MsgLogoutRespond {
	return &MsgLogoutRespond{}
}

func (m *MsgLogoutRespond) SetUserstatus(items []*MsgLogoutResult) { m.UserStatus = items }
func (m *MsgLogoutRespond) AddUserstatus(item *MsgLogoutResult) {
	m.UserStatus = append(m.UserStatus, item)
}

func (m *MsgLogoutRespond) Userstatus(index int) *MsgLogoutResult {
	if index < 0 || index >= len(m.UserStatus) {
		return NewMsgLogoutResult()
	}
	return m.UserStatus[index]
}

func (m *MsgLogoutRespond) UserstatusSize() int { return len(m.UserStatus) }
func (m *MsgLogoutRespond) HasUserstatus() bool { return m.UserStatus != nil }

func (m *MsgLogoutRespond) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgLogoutRespond) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgLogoutRespond) ParseFromRaw(raw *MsgLogoutRespond) error {
	if raw == nil {
		return nil
	}
	if raw.UserStatus != nil {
		m.UserStatus = make([]*MsgLogoutResult, len(raw.UserStatus))
		for i, item := range raw.UserStatus {
			cp := NewMsgLogoutResult()
			cp.ParseFromRaw(item)
			m.UserStatus[i] = cp
		}
	}
	return nil
}

// ---- move.go ----
type MsgMoveStatus int

const (
	MOVE_NO_ERROR MsgMoveStatus = 0
	MOVE_ID_WRONG MsgMoveStatus = -1
)

type MsgMoveUserInfo struct {
	ID      *int `json:"id,omitempty"`
	Village *int `json:"village,omitempty"`
	Area    *int `json:"area,omitempty"`
	X       *int `json:"x,omitempty"`
	Y       *int `json:"y,omitempty"`
	Type    *int `json:"type,omitempty"`
	Speed   *int `json:"speed,omitempty"`
}

func NewMsgMoveUserInfo() *MsgMoveUserInfo {
	return &MsgMoveUserInfo{}
}

func (m *MsgMoveUserInfo) SetId(v int) { m.ID = &v }
func (m *MsgMoveUserInfo) Id() int {
	if m.ID != nil {
		return *m.ID
	}
	return 0
}
func (m *MsgMoveUserInfo) HasId() bool      { return m.ID != nil }
func (m *MsgMoveUserInfo) SetVillage(v int) { m.Village = &v }
func (m *MsgMoveUserInfo) GetVillage() int {
	if m.Village != nil {
		return *m.Village
	}
	return 0
}
func (m *MsgMoveUserInfo) HasVillage() bool { return m.Village != nil }
func (m *MsgMoveUserInfo) SetArea(v int)    { m.Area = &v }
func (m *MsgMoveUserInfo) GetArea() int {
	if m.Area != nil {
		return *m.Area
	}
	return 0
}
func (m *MsgMoveUserInfo) HasArea() bool { return m.Area != nil }
func (m *MsgMoveUserInfo) SetX(v int)    { m.X = &v }
func (m *MsgMoveUserInfo) GetX() int {
	if m.X != nil {
		return *m.X
	}
	return 0
}
func (m *MsgMoveUserInfo) HasX() bool { return m.X != nil }
func (m *MsgMoveUserInfo) SetY(v int) { m.Y = &v }
func (m *MsgMoveUserInfo) GetY() int {
	if m.Y != nil {
		return *m.Y
	}
	return 0
}
func (m *MsgMoveUserInfo) HasY() bool    { return m.Y != nil }
func (m *MsgMoveUserInfo) SetType(v int) { m.Type = &v }
func (m *MsgMoveUserInfo) TypeVal() int {
	if m.Type != nil {
		return *m.Type
	}
	return 0
}
func (m *MsgMoveUserInfo) HasType() bool  { return m.Type != nil }
func (m *MsgMoveUserInfo) SetSpeed(v int) { m.Speed = &v }
func (m *MsgMoveUserInfo) GetSpeed() int {
	if m.Speed != nil {
		return *m.Speed
	}
	return 0
}
func (m *MsgMoveUserInfo) HasSpeed() bool { return m.Speed != nil }

func (m *MsgMoveUserInfo) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgMoveUserInfo) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgMoveUserInfo) ParseFromRaw(raw *MsgMoveUserInfo) error {
	if raw == nil {
		return nil
	}
	if raw.ID != nil {
		v := *raw.ID
		m.ID = &v
	}
	if raw.Village != nil {
		v := *raw.Village
		m.Village = &v
	}
	if raw.Area != nil {
		v := *raw.Area
		m.Area = &v
	}
	if raw.X != nil {
		v := *raw.X
		m.X = &v
	}
	if raw.Y != nil {
		v := *raw.Y
		m.Y = &v
	}
	if raw.Type != nil {
		v := *raw.Type
		m.Type = &v
	}
	if raw.Speed != nil {
		v := *raw.Speed
		m.Speed = &v
	}
	return nil
}

type MsgMoveResult struct {
	ID     *int           `json:"id,omitempty"`
	Status *MsgMoveStatus `json:"status,omitempty"`
}

func NewMsgMoveResult() *MsgMoveResult {
	return &MsgMoveResult{}
}

func (m *MsgMoveResult) SetId(v int) { m.ID = &v }
func (m *MsgMoveResult) Id() int {
	if m.ID != nil {
		return *m.ID
	}
	return 0
}
func (m *MsgMoveResult) HasId() bool               { return m.ID != nil }
func (m *MsgMoveResult) SetStatus(v MsgMoveStatus) { m.Status = &v }
func (m *MsgMoveResult) GetStatus() MsgMoveStatus {
	if m.Status != nil {
		return *m.Status
	}
	return 0
}
func (m *MsgMoveResult) HasStatus() bool { return m.Status != nil }

func (m *MsgMoveResult) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgMoveResult) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgMoveResult) ParseFromRaw(raw *MsgMoveResult) error {
	if raw == nil {
		return nil
	}
	if raw.ID != nil {
		v := *raw.ID
		m.ID = &v
	}
	if raw.Status != nil {
		v := *raw.Status
		m.Status = &v
	}
	return nil
}

type MsgMoveRequest struct {
	Key       *string            `json:"key,omitempty"`
	UserInfos []*MsgMoveUserInfo `json:"userinfos,omitempty"`
}

func NewMsgMoveRequest() *MsgMoveRequest {
	return &MsgMoveRequest{}
}

func (m *MsgMoveRequest) SetKey(v string) { m.Key = &v }
func (m *MsgMoveRequest) GetKey() string {
	if m.Key != nil {
		return *m.Key
	}
	return ""
}
func (m *MsgMoveRequest) HasKey() bool { return m.Key != nil }

func (m *MsgMoveRequest) SetUserinfos(items []*MsgMoveUserInfo) { m.UserInfos = items }
func (m *MsgMoveRequest) AddUserinfos(item *MsgMoveUserInfo)    { m.UserInfos = append(m.UserInfos, item) }

func (m *MsgMoveRequest) Userinfos(index int) *MsgMoveUserInfo {
	if index < 0 || index >= len(m.UserInfos) {
		return NewMsgMoveUserInfo()
	}
	return m.UserInfos[index]
}

func (m *MsgMoveRequest) UserinfosSize() int { return len(m.UserInfos) }
func (m *MsgMoveRequest) HasUserinfos() bool { return m.UserInfos != nil }

func (m *MsgMoveRequest) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgMoveRequest) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgMoveRequest) ParseFromRaw(raw *MsgMoveRequest) error {
	if raw == nil {
		return nil
	}
	if raw.Key != nil {
		v := *raw.Key
		m.Key = &v
	}
	if raw.UserInfos != nil {
		m.UserInfos = make([]*MsgMoveUserInfo, len(raw.UserInfos))
		for i, item := range raw.UserInfos {
			cp := NewMsgMoveUserInfo()
			cp.ParseFromRaw(item)
			m.UserInfos[i] = cp
		}
	}
	return nil
}

type MsgMoveRespond struct {
	UserStatus []*MsgMoveResult `json:"userstatus,omitempty"`
}

func NewMsgMoveRespond() *MsgMoveRespond {
	return &MsgMoveRespond{}
}

func (m *MsgMoveRespond) SetUserstatus(items []*MsgMoveResult) { m.UserStatus = items }
func (m *MsgMoveRespond) AddUserstatus(item *MsgMoveResult) {
	m.UserStatus = append(m.UserStatus, item)
}

func (m *MsgMoveRespond) Userstatus(index int) *MsgMoveResult {
	if index < 0 || index >= len(m.UserStatus) {
		return NewMsgMoveResult()
	}
	return m.UserStatus[index]
}

func (m *MsgMoveRespond) UserstatusSize() int { return len(m.UserStatus) }
func (m *MsgMoveRespond) HasUserstatus() bool { return m.UserStatus != nil }

func (m *MsgMoveRespond) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgMoveRespond) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgMoveRespond) ParseFromRaw(raw *MsgMoveRespond) error {
	if raw == nil {
		return nil
	}
	if raw.UserStatus != nil {
		m.UserStatus = make([]*MsgMoveResult, len(raw.UserStatus))
		for i, item := range raw.UserStatus {
			cp := NewMsgMoveResult()
			cp.ParseFromRaw(item)
			m.UserStatus[i] = cp
		}
	}
	return nil
}

// ---- online.go ----
type MsgOnLineStatus int

const (
	ONLINE_NO_ERROR    MsgOnLineStatus = 0
	ONLINE_MAX_REACHED MsgOnLineStatus = -1
)

type MsgOnLineStoreItem struct {
	Index    *int `json:"index,omitempty"`
	BoxType  *int `json:"boxtype,omitempty"`
	BoxIndex *int `json:"boxindex,omitempty"`
	Price    *int `json:"price,omitempty"`
	Count    *int `json:"count,omitempty"`
}

func NewMsgOnLineStoreItem() *MsgOnLineStoreItem {
	return &MsgOnLineStoreItem{}
}

func (m *MsgOnLineStoreItem) SetIndex(v int) { m.Index = &v }
func (m *MsgOnLineStoreItem) GetIndex() int {
	if m.Index != nil {
		return *m.Index
	}
	return 0
}
func (m *MsgOnLineStoreItem) HasIndex() bool   { return m.Index != nil }
func (m *MsgOnLineStoreItem) SetBoxtype(v int) { m.BoxType = &v }
func (m *MsgOnLineStoreItem) Boxtype() int {
	if m.BoxType != nil {
		return *m.BoxType
	}
	return 0
}
func (m *MsgOnLineStoreItem) HasBoxtype() bool  { return m.BoxType != nil }
func (m *MsgOnLineStoreItem) SetBoxindex(v int) { m.BoxIndex = &v }
func (m *MsgOnLineStoreItem) Boxindex() int {
	if m.BoxIndex != nil {
		return *m.BoxIndex
	}
	return 0
}
func (m *MsgOnLineStoreItem) HasBoxindex() bool { return m.BoxIndex != nil }
func (m *MsgOnLineStoreItem) SetPrice(v int)    { m.Price = &v }
func (m *MsgOnLineStoreItem) GetPrice() int {
	if m.Price != nil {
		return *m.Price
	}
	return 0
}
func (m *MsgOnLineStoreItem) HasPrice() bool { return m.Price != nil }
func (m *MsgOnLineStoreItem) SetCount(v int) { m.Count = &v }
func (m *MsgOnLineStoreItem) GetCount() int {
	if m.Count != nil {
		return *m.Count
	}
	return 0
}
func (m *MsgOnLineStoreItem) HasCount() bool { return m.Count != nil }

func (m *MsgOnLineStoreItem) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgOnLineStoreItem) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgOnLineStoreItem) ParseFromRaw(raw *MsgOnLineStoreItem) error {
	if raw == nil {
		return nil
	}
	if raw.Index != nil {
		v := *raw.Index
		m.Index = &v
	}
	if raw.BoxType != nil {
		v := *raw.BoxType
		m.BoxType = &v
	}
	if raw.BoxIndex != nil {
		v := *raw.BoxIndex
		m.BoxIndex = &v
	}
	if raw.Price != nil {
		v := *raw.Price
		m.Price = &v
	}
	if raw.Count != nil {
		v := *raw.Count
		m.Count = &v
	}
	return nil
}

type MsgOnLineUserInfo struct {
	ID         *int                  `json:"id,omitempty"`
	IP         *string               `json:"ip,omitempty"`
	Port       *int                  `json:"port,omitempty"`
	Delay      *int                  `json:"delay,omitempty"`
	Token      *string               `json:"token,omitempty"`
	UID        *int                  `json:"uid,omitempty"`
	CID        *int                  `json:"cid,omitempty"`
	MaxReconn  *int                  `json:"maxreconn,omitempty"`
	ReDelay    *int                  `json:"redelay,omitempty"`
	BirthVill  *int                  `json:"birthvill,omitempty"`
	BirthArea  *int                  `json:"birtharea,omitempty"`
	BirthX     *int                  `json:"birthx,omitempty"`
	BirthY     *int                  `json:"birthy,omitempty"`
	DisOpen    *int                  `json:"disopen,omitempty"`
	DisCost    *int                  `json:"discost,omitempty"`
	StoreOpen  *int                  `json:"storeopen,omitempty"`
	StoreTitle *string               `json:"storetitle,omitempty"`
	StoreInfo  []*MsgOnLineStoreItem `json:"storeinfo,omitempty"`
}

func NewMsgOnLineUserInfo() *MsgOnLineUserInfo {
	return &MsgOnLineUserInfo{}
}

func (m *MsgOnLineUserInfo) SetId(v int) { m.ID = &v }
func (m *MsgOnLineUserInfo) Id() int {
	if m.ID != nil {
		return *m.ID
	}
	return 0
}
func (m *MsgOnLineUserInfo) HasId() bool    { return m.ID != nil }
func (m *MsgOnLineUserInfo) SetIp(v string) { m.IP = &v }
func (m *MsgOnLineUserInfo) Ip() string {
	if m.IP != nil {
		return *m.IP
	}
	return ""
}
func (m *MsgOnLineUserInfo) HasIp() bool   { return m.IP != nil }
func (m *MsgOnLineUserInfo) SetPort(v int) { m.Port = &v }
func (m *MsgOnLineUserInfo) GetPort() int {
	if m.Port != nil {
		return *m.Port
	}
	return 0
}
func (m *MsgOnLineUserInfo) HasPort() bool  { return m.Port != nil }
func (m *MsgOnLineUserInfo) SetDelay(v int) { m.Delay = &v }
func (m *MsgOnLineUserInfo) GetDelay() int {
	if m.Delay != nil {
		return *m.Delay
	}
	return 0
}
func (m *MsgOnLineUserInfo) HasDelay() bool    { return m.Delay != nil }
func (m *MsgOnLineUserInfo) SetToken(v string) { m.Token = &v }
func (m *MsgOnLineUserInfo) GetToken() string {
	if m.Token != nil {
		return *m.Token
	}
	return ""
}
func (m *MsgOnLineUserInfo) HasToken() bool { return m.Token != nil }
func (m *MsgOnLineUserInfo) SetUid(v int)   { m.UID = &v }
func (m *MsgOnLineUserInfo) Uid() int {
	if m.UID != nil {
		return *m.UID
	}
	return 0
}
func (m *MsgOnLineUserInfo) HasUid() bool { return m.UID != nil }
func (m *MsgOnLineUserInfo) SetCid(v int) { m.CID = &v }
func (m *MsgOnLineUserInfo) Cid() int {
	if m.CID != nil {
		return *m.CID
	}
	return 0
}
func (m *MsgOnLineUserInfo) HasCid() bool       { return m.CID != nil }
func (m *MsgOnLineUserInfo) SetMaxreconn(v int) { m.MaxReconn = &v }
func (m *MsgOnLineUserInfo) Maxreconn() int {
	if m.MaxReconn != nil {
		return *m.MaxReconn
	}
	return 0
}
func (m *MsgOnLineUserInfo) HasMaxreconn() bool { return m.MaxReconn != nil }
func (m *MsgOnLineUserInfo) SetRedelay(v int)   { m.ReDelay = &v }
func (m *MsgOnLineUserInfo) Redelay() int {
	if m.ReDelay != nil {
		return *m.ReDelay
	}
	return 0
}
func (m *MsgOnLineUserInfo) HasRedelay() bool   { return m.ReDelay != nil }
func (m *MsgOnLineUserInfo) SetBirthvill(v int) { m.BirthVill = &v }
func (m *MsgOnLineUserInfo) Birthvill() int {
	if m.BirthVill != nil {
		return *m.BirthVill
	}
	return 0
}
func (m *MsgOnLineUserInfo) HasBirthvill() bool { return m.BirthVill != nil }
func (m *MsgOnLineUserInfo) SetBirtharea(v int) { m.BirthArea = &v }
func (m *MsgOnLineUserInfo) Birtharea() int {
	if m.BirthArea != nil {
		return *m.BirthArea
	}
	return 0
}
func (m *MsgOnLineUserInfo) HasBirtharea() bool { return m.BirthArea != nil }
func (m *MsgOnLineUserInfo) SetBirthx(v int)    { m.BirthX = &v }
func (m *MsgOnLineUserInfo) Birthx() int {
	if m.BirthX != nil {
		return *m.BirthX
	}
	return 0
}
func (m *MsgOnLineUserInfo) HasBirthx() bool { return m.BirthX != nil }
func (m *MsgOnLineUserInfo) SetBirthy(v int) { m.BirthY = &v }
func (m *MsgOnLineUserInfo) Birthy() int {
	if m.BirthY != nil {
		return *m.BirthY
	}
	return 0
}
func (m *MsgOnLineUserInfo) HasBirthy() bool  { return m.BirthY != nil }
func (m *MsgOnLineUserInfo) SetDisopen(v int) { m.DisOpen = &v }
func (m *MsgOnLineUserInfo) Disopen() int {
	if m.DisOpen != nil {
		return *m.DisOpen
	}
	return 0
}
func (m *MsgOnLineUserInfo) HasDisopen() bool { return m.DisOpen != nil }
func (m *MsgOnLineUserInfo) SetDiscost(v int) { m.DisCost = &v }
func (m *MsgOnLineUserInfo) Discost() int {
	if m.DisCost != nil {
		return *m.DisCost
	}
	return 0
}
func (m *MsgOnLineUserInfo) HasDiscost() bool   { return m.DisCost != nil }
func (m *MsgOnLineUserInfo) SetStoreopen(v int) { m.StoreOpen = &v }
func (m *MsgOnLineUserInfo) Storeopen() int {
	if m.StoreOpen != nil {
		return *m.StoreOpen
	}
	return 0
}
func (m *MsgOnLineUserInfo) HasStoreopen() bool     { return m.StoreOpen != nil }
func (m *MsgOnLineUserInfo) SetStoretitle(v string) { m.StoreTitle = &v }
func (m *MsgOnLineUserInfo) Storetitle() string {
	if m.StoreTitle != nil {
		return *m.StoreTitle
	}
	return ""
}
func (m *MsgOnLineUserInfo) HasStoretitle() bool { return m.StoreTitle != nil }

func (m *MsgOnLineUserInfo) SetStoreinfo(items []*MsgOnLineStoreItem) { m.StoreInfo = items }
func (m *MsgOnLineUserInfo) AddStoreinfo(item *MsgOnLineStoreItem) {
	m.StoreInfo = append(m.StoreInfo, item)
}

func (m *MsgOnLineUserInfo) StoreinfoItem(index int) *MsgOnLineStoreItem {
	if index < 0 || index >= len(m.StoreInfo) {
		return NewMsgOnLineStoreItem()
	}
	return m.StoreInfo[index]
}

func (m *MsgOnLineUserInfo) StoreinfoSize() int { return len(m.StoreInfo) }
func (m *MsgOnLineUserInfo) HasStoreinfo() bool { return m.StoreInfo != nil }

func (m *MsgOnLineUserInfo) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgOnLineUserInfo) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgOnLineUserInfo) ParseFromRaw(raw *MsgOnLineUserInfo) error {
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
	if raw.Delay != nil {
		v := *raw.Delay
		m.Delay = &v
	}
	if raw.Token != nil {
		v := *raw.Token
		m.Token = &v
	}
	if raw.UID != nil {
		v := *raw.UID
		m.UID = &v
	}
	if raw.CID != nil {
		v := *raw.CID
		m.CID = &v
	}
	if raw.MaxReconn != nil {
		v := *raw.MaxReconn
		m.MaxReconn = &v
	}
	if raw.ReDelay != nil {
		v := *raw.ReDelay
		m.ReDelay = &v
	}
	if raw.BirthVill != nil {
		v := *raw.BirthVill
		m.BirthVill = &v
	}
	if raw.BirthArea != nil {
		v := *raw.BirthArea
		m.BirthArea = &v
	}
	if raw.BirthX != nil {
		v := *raw.BirthX
		m.BirthX = &v
	}
	if raw.BirthY != nil {
		v := *raw.BirthY
		m.BirthY = &v
	}
	if raw.DisOpen != nil {
		v := *raw.DisOpen
		m.DisOpen = &v
	}
	if raw.DisCost != nil {
		v := *raw.DisCost
		m.DisCost = &v
	}
	if raw.StoreOpen != nil {
		v := *raw.StoreOpen
		m.StoreOpen = &v
	}
	if raw.StoreTitle != nil {
		v := *raw.StoreTitle
		m.StoreTitle = &v
	}
	if raw.StoreInfo != nil {
		m.StoreInfo = make([]*MsgOnLineStoreItem, len(raw.StoreInfo))
		for i, item := range raw.StoreInfo {
			cp := NewMsgOnLineStoreItem()
			cp.ParseFromRaw(item)
			m.StoreInfo[i] = cp
		}
	}
	return nil
}

type MsgOnLineResult struct {
	ID     *int             `json:"id,omitempty"`
	Status *MsgOnLineStatus `json:"status,omitempty"`
}

func NewMsgOnLineResult() *MsgOnLineResult {
	return &MsgOnLineResult{}
}

func (m *MsgOnLineResult) SetId(v int) { m.ID = &v }
func (m *MsgOnLineResult) Id() int {
	if m.ID != nil {
		return *m.ID
	}
	return 0
}
func (m *MsgOnLineResult) HasId() bool                 { return m.ID != nil }
func (m *MsgOnLineResult) SetStatus(v MsgOnLineStatus) { m.Status = &v }
func (m *MsgOnLineResult) GetStatus() MsgOnLineStatus {
	if m.Status != nil {
		return *m.Status
	}
	return 0
}
func (m *MsgOnLineResult) HasStatus() bool { return m.Status != nil }

func (m *MsgOnLineResult) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgOnLineResult) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgOnLineResult) ParseFromRaw(raw *MsgOnLineResult) error {
	if raw == nil {
		return nil
	}
	if raw.ID != nil {
		v := *raw.ID
		m.ID = &v
	}
	if raw.Status != nil {
		v := *raw.Status
		m.Status = &v
	}
	return nil
}

type MsgOnLineRequest struct {
	Key       *string              `json:"key,omitempty"`
	UserInfos []*MsgOnLineUserInfo `json:"userinfos,omitempty"`
}

func NewMsgOnLineRequest() *MsgOnLineRequest {
	return &MsgOnLineRequest{}
}

func (m *MsgOnLineRequest) SetKey(v string) { m.Key = &v }
func (m *MsgOnLineRequest) GetKey() string {
	if m.Key != nil {
		return *m.Key
	}
	return ""
}
func (m *MsgOnLineRequest) HasKey() bool { return m.Key != nil }

func (m *MsgOnLineRequest) SetUserinfos(items []*MsgOnLineUserInfo) { m.UserInfos = items }
func (m *MsgOnLineRequest) AddUserinfos(item *MsgOnLineUserInfo) {
	m.UserInfos = append(m.UserInfos, item)
}

func (m *MsgOnLineRequest) Userinfos(index int) *MsgOnLineUserInfo {
	if index < 0 || index >= len(m.UserInfos) {
		return NewMsgOnLineUserInfo()
	}
	return m.UserInfos[index]
}

func (m *MsgOnLineRequest) UserinfosSize() int { return len(m.UserInfos) }
func (m *MsgOnLineRequest) HasUserinfos() bool { return m.UserInfos != nil }

func (m *MsgOnLineRequest) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgOnLineRequest) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgOnLineRequest) ParseFromRaw(raw *MsgOnLineRequest) error {
	if raw == nil {
		return nil
	}
	if raw.Key != nil {
		v := *raw.Key
		m.Key = &v
	}
	if raw.UserInfos != nil {
		m.UserInfos = make([]*MsgOnLineUserInfo, len(raw.UserInfos))
		for i, item := range raw.UserInfos {
			cp := NewMsgOnLineUserInfo()
			cp.ParseFromRaw(item)
			m.UserInfos[i] = cp
		}
	}
	return nil
}

type MsgOnLineRespond struct {
	UserStatus []*MsgOnLineResult `json:"userstatus,omitempty"`
}

func NewMsgOnLineRespond() *MsgOnLineRespond {
	return &MsgOnLineRespond{}
}

func (m *MsgOnLineRespond) SetUserstatus(items []*MsgOnLineResult) { m.UserStatus = items }
func (m *MsgOnLineRespond) AddUserstatus(item *MsgOnLineResult) {
	m.UserStatus = append(m.UserStatus, item)
}

func (m *MsgOnLineRespond) Userstatus(index int) *MsgOnLineResult {
	if index < 0 || index >= len(m.UserStatus) {
		return NewMsgOnLineResult()
	}
	return m.UserStatus[index]
}

func (m *MsgOnLineRespond) UserstatusSize() int { return len(m.UserStatus) }
func (m *MsgOnLineRespond) HasUserstatus() bool { return m.UserStatus != nil }

func (m *MsgOnLineRespond) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgOnLineRespond) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgOnLineRespond) ParseFromRaw(raw *MsgOnLineRespond) error {
	if raw == nil {
		return nil
	}
	if raw.UserStatus != nil {
		m.UserStatus = make([]*MsgOnLineResult, len(raw.UserStatus))
		for i, item := range raw.UserStatus {
			cp := NewMsgOnLineResult()
			cp.ParseFromRaw(item)
			m.UserStatus[i] = cp
		}
	}
	return nil
}

// ---- public.go ----
type MsgPublicMsgStatus int

const (
	PUBLIC_MSG_NO_ERROR MsgPublicMsgStatus = 0
	PUBLIC_MSG_ID_WRONG MsgPublicMsgStatus = -1
)

type MsgPublicMsgUserInfo struct {
	ID      *int    `json:"id,omitempty"`
	Message *string `json:"msg,omitempty"`
	Type    *int    `json:"type,omitempty"`
}

func NewMsgPublicMsgUserInfo() *MsgPublicMsgUserInfo {
	return &MsgPublicMsgUserInfo{}
}

func (m *MsgPublicMsgUserInfo) SetId(v int) { m.ID = &v }
func (m *MsgPublicMsgUserInfo) Id() int {
	if m.ID != nil {
		return *m.ID
	}
	return 0
}
func (m *MsgPublicMsgUserInfo) HasId() bool     { return m.ID != nil }
func (m *MsgPublicMsgUserInfo) SetMsg(v string) { m.Message = &v }
func (m *MsgPublicMsgUserInfo) Msg() string {
	if m.Message != nil {
		return *m.Message
	}
	return ""
}
func (m *MsgPublicMsgUserInfo) HasMsg() bool  { return m.Message != nil }
func (m *MsgPublicMsgUserInfo) SetType(v int) { m.Type = &v }
func (m *MsgPublicMsgUserInfo) TypeValue() int {
	if m.Type != nil {
		return *m.Type
	}
	return 3
}
func (m *MsgPublicMsgUserInfo) HasType() bool { return m.Type != nil }

func (m *MsgPublicMsgUserInfo) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgPublicMsgUserInfo) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgPublicMsgUserInfo) ParseFromRaw(raw *MsgPublicMsgUserInfo) error {
	if raw == nil {
		return nil
	}
	if raw.ID != nil {
		v := *raw.ID
		m.ID = &v
	}
	if raw.Message != nil {
		v := *raw.Message
		m.Message = &v
	}
	if raw.Type != nil {
		v := *raw.Type
		m.Type = &v
	}
	return nil
}

type MsgPublicMsgResult struct {
	ID     *int                `json:"id,omitempty"`
	Status *MsgPublicMsgStatus `json:"status,omitempty"`
}

func NewMsgPublicMsgResult() *MsgPublicMsgResult {
	return &MsgPublicMsgResult{}
}

func (m *MsgPublicMsgResult) SetId(v int) { m.ID = &v }
func (m *MsgPublicMsgResult) Id() int {
	if m.ID != nil {
		return *m.ID
	}
	return 0
}
func (m *MsgPublicMsgResult) HasId() bool                    { return m.ID != nil }
func (m *MsgPublicMsgResult) SetStatus(v MsgPublicMsgStatus) { m.Status = &v }
func (m *MsgPublicMsgResult) GetStatus() MsgPublicMsgStatus {
	if m.Status != nil {
		return *m.Status
	}
	return 0
}
func (m *MsgPublicMsgResult) HasStatus() bool { return m.Status != nil }

func (m *MsgPublicMsgResult) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgPublicMsgResult) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgPublicMsgResult) ParseFromRaw(raw *MsgPublicMsgResult) error {
	if raw == nil {
		return nil
	}
	if raw.ID != nil {
		v := *raw.ID
		m.ID = &v
	}
	if raw.Status != nil {
		v := *raw.Status
		m.Status = &v
	}
	return nil
}

type MsgPublicMsgRequest struct {
	Key       *string                 `json:"key,omitempty"`
	UserInfos []*MsgPublicMsgUserInfo `json:"userinfos,omitempty"`
}

func NewMsgPublicMsgRequest() *MsgPublicMsgRequest {
	return &MsgPublicMsgRequest{}
}

func (m *MsgPublicMsgRequest) SetKey(v string) { m.Key = &v }
func (m *MsgPublicMsgRequest) GetKey() string {
	if m.Key != nil {
		return *m.Key
	}
	return ""
}
func (m *MsgPublicMsgRequest) HasKey() bool { return m.Key != nil }

func (m *MsgPublicMsgRequest) SetUserinfos(items []*MsgPublicMsgUserInfo) { m.UserInfos = items }
func (m *MsgPublicMsgRequest) AddUserinfos(item *MsgPublicMsgUserInfo) {
	m.UserInfos = append(m.UserInfos, item)
}

func (m *MsgPublicMsgRequest) Userinfos(index int) *MsgPublicMsgUserInfo {
	if index < 0 || index >= len(m.UserInfos) {
		return NewMsgPublicMsgUserInfo()
	}
	return m.UserInfos[index]
}

func (m *MsgPublicMsgRequest) UserinfosSize() int { return len(m.UserInfos) }
func (m *MsgPublicMsgRequest) HasUserinfos() bool { return m.UserInfos != nil }

func (m *MsgPublicMsgRequest) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgPublicMsgRequest) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgPublicMsgRequest) ParseFromRaw(raw *MsgPublicMsgRequest) error {
	if raw == nil {
		return nil
	}
	if raw.Key != nil {
		v := *raw.Key
		m.Key = &v
	}
	if raw.UserInfos != nil {
		m.UserInfos = make([]*MsgPublicMsgUserInfo, len(raw.UserInfos))
		for i, item := range raw.UserInfos {
			cp := NewMsgPublicMsgUserInfo()
			cp.ParseFromRaw(item)
			m.UserInfos[i] = cp
		}
	}
	return nil
}

type MsgPublicMsgRespond struct {
	UserStatus []*MsgPublicMsgResult `json:"userstatus,omitempty"`
}

func NewMsgPublicMsgRespond() *MsgPublicMsgRespond {
	return &MsgPublicMsgRespond{}
}

func (m *MsgPublicMsgRespond) SetUserstatus(items []*MsgPublicMsgResult) { m.UserStatus = items }
func (m *MsgPublicMsgRespond) AddUserstatus(item *MsgPublicMsgResult) {
	m.UserStatus = append(m.UserStatus, item)
}

func (m *MsgPublicMsgRespond) Userstatus(index int) *MsgPublicMsgResult {
	if index < 0 || index >= len(m.UserStatus) {
		return NewMsgPublicMsgResult()
	}
	return m.UserStatus[index]
}

func (m *MsgPublicMsgRespond) UserstatusSize() int { return len(m.UserStatus) }
func (m *MsgPublicMsgRespond) HasUserstatus() bool { return m.UserStatus != nil }

func (m *MsgPublicMsgRespond) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgPublicMsgRespond) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgPublicMsgRespond) ParseFromRaw(raw *MsgPublicMsgRespond) error {
	if raw == nil {
		return nil
	}
	if raw.UserStatus != nil {
		m.UserStatus = make([]*MsgPublicMsgResult, len(raw.UserStatus))
		for i, item := range raw.UserStatus {
			cp := NewMsgPublicMsgResult()
			cp.ParseFromRaw(item)
			m.UserStatus[i] = cp
		}
	}
	return nil
}

// ---- remove.go ----
type MsgRemoveStatus int

const (
	REMOVE_NO_ERROR MsgRemoveStatus = 0
	REMOVE_ID_WRONG MsgRemoveStatus = -1
)

type MsgRemoveUserInfo struct {
	ID *int `json:"id,omitempty"`
}

func NewMsgRemoveUserInfo() *MsgRemoveUserInfo {
	return &MsgRemoveUserInfo{}
}

func (m *MsgRemoveUserInfo) SetId(v int) { m.ID = &v }
func (m *MsgRemoveUserInfo) Id() int {
	if m.ID != nil {
		return *m.ID
	}
	return 0
}
func (m *MsgRemoveUserInfo) HasId() bool { return m.ID != nil }

func (m *MsgRemoveUserInfo) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgRemoveUserInfo) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgRemoveUserInfo) ParseFromRaw(raw *MsgRemoveUserInfo) error {
	if raw == nil {
		return nil
	}
	if raw.ID != nil {
		v := *raw.ID
		m.ID = &v
	}
	return nil
}

type MsgRemoveResult struct {
	ID     *int             `json:"id,omitempty"`
	Status *MsgRemoveStatus `json:"status,omitempty"`
}

func NewMsgRemoveResult() *MsgRemoveResult {
	return &MsgRemoveResult{}
}

func (m *MsgRemoveResult) SetId(v int) { m.ID = &v }
func (m *MsgRemoveResult) Id() int {
	if m.ID != nil {
		return *m.ID
	}
	return 0
}
func (m *MsgRemoveResult) HasId() bool                 { return m.ID != nil }
func (m *MsgRemoveResult) SetStatus(v MsgRemoveStatus) { m.Status = &v }
func (m *MsgRemoveResult) GetStatus() MsgRemoveStatus {
	if m.Status != nil {
		return *m.Status
	}
	return 0
}
func (m *MsgRemoveResult) HasStatus() bool { return m.Status != nil }

func (m *MsgRemoveResult) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgRemoveResult) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgRemoveResult) ParseFromRaw(raw *MsgRemoveResult) error {
	if raw == nil {
		return nil
	}
	if raw.ID != nil {
		v := *raw.ID
		m.ID = &v
	}
	if raw.Status != nil {
		v := *raw.Status
		m.Status = &v
	}
	return nil
}

type MsgRemoveRequest struct {
	Key       *string              `json:"key,omitempty"`
	UserInfos []*MsgRemoveUserInfo `json:"userinfos,omitempty"`
}

func NewMsgRemoveRequest() *MsgRemoveRequest {
	return &MsgRemoveRequest{}
}

func (m *MsgRemoveRequest) SetKey(v string) { m.Key = &v }
func (m *MsgRemoveRequest) GetKey() string {
	if m.Key != nil {
		return *m.Key
	}
	return ""
}
func (m *MsgRemoveRequest) HasKey() bool { return m.Key != nil }

func (m *MsgRemoveRequest) SetUserinfos(items []*MsgRemoveUserInfo) { m.UserInfos = items }
func (m *MsgRemoveRequest) AddUserinfos(item *MsgRemoveUserInfo) {
	m.UserInfos = append(m.UserInfos, item)
}

func (m *MsgRemoveRequest) Userinfos(index int) *MsgRemoveUserInfo {
	if index < 0 || index >= len(m.UserInfos) {
		return NewMsgRemoveUserInfo()
	}
	return m.UserInfos[index]
}

func (m *MsgRemoveRequest) UserinfosSize() int { return len(m.UserInfos) }
func (m *MsgRemoveRequest) HasUserinfos() bool { return m.UserInfos != nil }

func (m *MsgRemoveRequest) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgRemoveRequest) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgRemoveRequest) ParseFromRaw(raw *MsgRemoveRequest) error {
	if raw == nil {
		return nil
	}
	if raw.Key != nil {
		v := *raw.Key
		m.Key = &v
	}
	if raw.UserInfos != nil {
		m.UserInfos = make([]*MsgRemoveUserInfo, len(raw.UserInfos))
		for i, item := range raw.UserInfos {
			cp := NewMsgRemoveUserInfo()
			cp.ParseFromRaw(item)
			m.UserInfos[i] = cp
		}
	}
	return nil
}

type MsgRemoveRespond struct {
	UserStatus []*MsgRemoveResult `json:"userstatus,omitempty"`
}

func NewMsgRemoveRespond() *MsgRemoveRespond {
	return &MsgRemoveRespond{}
}

func (m *MsgRemoveRespond) SetUserstatus(items []*MsgRemoveResult) { m.UserStatus = items }
func (m *MsgRemoveRespond) AddUserstatus(item *MsgRemoveResult) {
	m.UserStatus = append(m.UserStatus, item)
}

func (m *MsgRemoveRespond) Userstatus(index int) *MsgRemoveResult {
	if index < 0 || index >= len(m.UserStatus) {
		return NewMsgRemoveResult()
	}
	return m.UserStatus[index]
}

func (m *MsgRemoveRespond) UserstatusSize() int { return len(m.UserStatus) }
func (m *MsgRemoveRespond) HasUserstatus() bool { return m.UserStatus != nil }

func (m *MsgRemoveRespond) SerializeToString() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *MsgRemoveRespond) ParseFromString(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *MsgRemoveRespond) ParseFromRaw(raw *MsgRemoveRespond) error {
	if raw == nil {
		return nil
	}
	if raw.UserStatus != nil {
		m.UserStatus = make([]*MsgRemoveResult, len(raw.UserStatus))
		for i, item := range raw.UserStatus {
			cp := NewMsgRemoveResult()
			cp.ParseFromRaw(item)
			m.UserStatus[i] = cp
		}
	}
	return nil
}
