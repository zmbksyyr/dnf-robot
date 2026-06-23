package message

import "encoding/json"

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
