package message

import "encoding/json"

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
