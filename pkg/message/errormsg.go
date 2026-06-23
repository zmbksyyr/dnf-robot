package message

import "encoding/json"

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
