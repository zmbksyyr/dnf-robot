package message

import "encoding/json"

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
