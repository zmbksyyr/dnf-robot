package message

import "encoding/json"

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
