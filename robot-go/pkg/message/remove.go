package message

import "encoding/json"

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
