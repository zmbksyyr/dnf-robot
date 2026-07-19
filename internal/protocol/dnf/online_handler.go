package dnf

import (
	"crypto/rsa"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"time"

	"robot/internal/foundation/crypto"
	"robot/internal/foundation/message"
	"robot/internal/shared"
)

var (
	rsaKey *rsa.PrivateKey
	dbPool *sql.DB
)

func SetRSAKey(key *rsa.PrivateKey) {
	rsaKey = key
}

func SetDBPool(db *sql.DB) {
	dbPool = db
}

func GetDBPool() *sql.DB {
	return dbPool
}

func (dt *DnfTableDrive) handleOnLine(task *RobotDnfTask, robotMsg RobotMsg) DnfTableTaskResult {
	req := message.NewMsgOnLineRequest()
	if err := req.ParseFromString(string(robotMsg.JSON)); err != nil || req.UserinfosSize() == 0 {
		return DnfTableTaskResult{Msg: invalidRequestMessage}
	}

	users := make([]shared.RuntimeOnlineUser, 0, req.UserinfosSize())
	resultIDs := make([]int, 0, req.UserinfosSize())
	for i := 0; i < req.UserinfosSize(); i++ {
		userInfo := req.Userinfos(i)
		if !completeOnlineUserInfo(userInfo) {
			return DnfTableTaskResult{Msg: invalidRequestMessage}
		}
		users = append(users, onlineUserFromMessage(userInfo))
		resultIDs = append(resultIDs, userInfo.Id())
	}

	result := dt.dispatchOnline(task, users)
	if result.Code != 200 {
		return result
	}
	respond := message.NewMsgOnLineRespond()
	for _, id := range resultIDs {
		item := message.NewMsgOnLineResult()
		item.SetId(id)
		item.SetStatus(message.ONLINE_NO_ERROR)
		respond.AddUserstatus(item)
	}
	result.Msg, _ = respond.SerializeToString()
	return result
}

func completeOnlineUserInfo(user *message.MsgOnLineUserInfo) bool {
	if user == nil || !user.HasId() || !user.HasIp() || !user.HasPort() || !user.HasDelay() ||
		!user.HasUid() || !user.HasCid() || !user.HasMaxreconn() || !user.HasRedelay() ||
		!user.HasBirthvill() || !user.HasBirtharea() || !user.HasBirthx() || !user.HasBirthy() ||
		!user.HasDisopen() || !user.HasStoreopen() {
		return false
	}
	if user.Disopen() != 0 && !user.HasDiscost() {
		return false
	}
	return user.Storeopen() == 0 || user.HasStoretitle()
}

func onlineUserFromMessage(user *message.MsgOnLineUserInfo) shared.RuntimeOnlineUser {
	return shared.RuntimeOnlineUser{
		IP:             user.Ip(),
		Port:           user.GetPort(),
		DelayMS:        user.GetDelay(),
		Token:          user.GetToken(),
		UID:            user.Uid(),
		CID:            user.Cid(),
		MaxReconnect:   user.Maxreconn(),
		ReconnectDelay: user.Redelay(),
		BirthVillage:   user.Birthvill(),
		BirthArea:      user.Birtharea(),
		BirthX:         user.Birthx(),
		BirthY:         user.Birthy(),
		DisjointOpen:   user.Disopen() != 0,
		DisjointCost:   user.Discost(),
		StoreOpen:      user.Storeopen() != 0,
		StoreTitle:     user.Storetitle(),
	}
}

func (dt *DnfTableDrive) DispatchOnline(users []shared.RuntimeOnlineUser) DnfTableTaskResult {
	return dt.dispatchOnline(dt.task, users)
}

func (dt *DnfTableDrive) dispatchOnline(task *RobotDnfTask, users []shared.RuntimeOnlineUser) DnfTableTaskResult {
	if task == nil || len(users) == 0 {
		return DnfTableTaskResult{Msg: invalidRequestMessage}
	}
	for _, user := range users {
		if !validOnlineUser(user) {
			return DnfTableTaskResult{Msg: invalidRequestMessage}
		}
	}
	db := dbPool
	if db == nil {
		return DnfTableTaskResult{Msg: "no database connection"}
	}
	for _, user := range users {
		if !repairLoginPrerequisites(db, user.UID, user.IP) {
			return DnfTableTaskResult{Msg: "repair login prerequisites failed"}
		}
	}
	if task.onlineBacklog()+len(users) > maxMessageQueueSize {
		return DnfTableTaskResult{Msg: "online queue full"}
	}

	for _, user := range users {
		loginInfo := UserLoginInfo{
			IP:        user.IP,
			Port:      user.Port,
			Delay:     uint32(user.DelayMS),
			UID:       uint32(user.UID),
			CID:       user.CID,
			MaxReConn: uint32(user.MaxReconnect),
			ReDelay:   uint32(user.ReconnectDelay),
			BirthPos: [4]uint32{
				uint32(user.BirthVillage),
				uint32(user.BirthArea),
				uint32(user.BirthX),
				uint32(user.BirthY),
			},
		}
		setLoginToken(&loginInfo, resolveLoginToken(user.Token, user.UID, rsaKey))

		vo := NewRobotVo(db)
		vo.Load(loginInfo)
		if user.DisjointOpen {
			vo.AfterRunAsyncTaskVec = append(vo.AfterRunAsyncTaskVec, AsyncTask{Type: AsyncDisjoint, Cost: user.DisjointCost})
		}
		if user.StoreOpen {
			vo.AfterRunAsyncTaskVec = append(vo.AfterRunAsyncTaskVec, AsyncTask{Type: AsyncPriStore, Title: user.StoreTitle})
		}
		if !task.TryAddMessage("MsgOnLine", vo) {
			return DnfTableTaskResult{Msg: "online queue full"}
		}
	}
	return DnfTableTaskResult{Code: 200}
}

func validOnlineUser(user shared.RuntimeOnlineUser) bool {
	return user.UID > 0 && len(user.IP) > 0 && len(user.IP) <= 15 &&
		user.Port > 0 && user.Port < 1<<16 && user.DelayMS >= 0 &&
		user.MaxReconnect >= 0 && user.ReconnectDelay >= 0 &&
		(!user.DisjointOpen || user.DisjointCost >= 0) &&
		(!user.StoreOpen || user.StoreTitle != "")
}

func resolveLoginToken(existing string, uid int, key *rsa.PrivateKey) string {
	if validLoginToken(existing, key) {
		return existing
	}
	if key == nil {
		return ""
	}
	token := generateLoginToken(uid, key)
	if token != "" {
		return token
	}
	token, err := crypto.GetLoginKey(uint32(uid), key)
	fmt.Printf("[TOKEN_ERR] primary token failed; fallback GetLoginKey len=%d err=%v\n", len(token), err)
	return token
}

func validLoginToken(token string, key *rsa.PrivateKey) bool {
	if token == "" {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(token)
	if err != nil || len(raw) == 0 || len(raw) > 512 {
		return false
	}
	return key == nil || len(raw) == key.Size()
}

func setLoginToken(info *UserLoginInfo, token string) {
	if info == nil || token == "" {
		return
	}
	tokenBytes := []byte(token)
	if len(tokenBytes) > len(info.Token) {
		tokenBytes = tokenBytes[:len(info.Token)]
	}
	copy(info.Token[:], tokenBytes)
	info.TokenSize = uint32(len(tokenBytes))
}

// generateLoginToken builds the token format used by the original server:
// bytes 0-3: swapByte32(uid)
// bytes 4-35: UID decimal string plus NUL padding
// bytes 36-39: current timestamp
// bytes 40-45: 0x01 0x04 0x03 0x03 0x01 0x01
func generateLoginToken(uid int, key *rsa.PrivateKey) string {
	if key == nil {
		return ""
	}
	now := uint32(time.Now().Unix())
	token := make([]byte, 46)
	binary.BigEndian.PutUint32(token[0:4], uint32(uid))
	copy(token[4:], []byte(fmt.Sprintf("%d", uid)))
	binary.BigEndian.PutUint32(token[36:40], now)
	token[40] = 0x01
	token[41] = 0x04
	token[42] = 0x03
	token[43] = 0x03
	token[44] = 0x01
	token[45] = 0x01
	sig, err := rsa.SignPKCS1v15(nil, key, 0, token)
	if err != nil {
		fmt.Printf("[TOKEN_ERR] RSA sign failed: %v\n", err)
		return ""
	}
	return base64.StdEncoding.EncodeToString(sig)
}
