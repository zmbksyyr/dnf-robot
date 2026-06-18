package service

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"sync"
	"time"
)

var getLoginKeyMu sync.Mutex

var privateKey *rsa.PrivateKey

const base64Chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

func InitPrivateKey(keyFile string) error {
	data, err := ioutil.ReadFile(keyFile)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return fmt.Errorf("failed to decode PEM")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		key2, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return fmt.Errorf("failed to parse private key: %v / %v", err, err2)
		}
		var ok bool
		privateKey, ok = key2.(*rsa.PrivateKey)
		if !ok {
			return fmt.Errorf("key is not RSA")
		}
	} else {
		privateKey = key
	}
	return nil
}

func ClosePrivateKey() {
	privateKey = nil
}

func GetRSAKey() *rsa.PrivateKey {
	return privateKey
}

func BuildLoginKeyPlainHex(uid int) string {
	now := uint32(time.Now().Unix())
	suffix := "010403030101"
	userName := fmt.Sprintf("%d", uid)
	userNameHex := ""
	for i := 0; i < 32; i++ {
		ch := byte(0)
		if i < len(userName) {
			ch = userName[i]
		}
		userNameHex += fmt.Sprintf("%02x", ch)
	}
	return fmt.Sprintf("%08x%s%08x%s", uid, userNameHex, now, suffix)
}

func Hex2Bin(hexStr string) []byte {
	result := make([]byte, len(hexStr)/2)
	for i := 0; i < len(hexStr); i += 2 {
		var b byte
		fmt.Sscanf(hexStr[i:i+2], "%02x", &b)
		result[i/2] = b
	}
	return result
}

func base64Encode(data []byte) string {
	result := make([]byte, 0, (len(data)+2)/3*4)
	for i := 0; i < len(data); i += 3 {
		current := (data[i] >> 2) & 0x3F
		result = append(result, base64Chars[current])

		current = (data[i] << 4) & 0x30
		if i+1 >= len(data) {
			result = append(result, base64Chars[current], '=', '=')
			break
		}
		current |= (data[i+1] >> 4) & 0x0F
		result = append(result, base64Chars[current])

		current = (data[i+1] << 2) & 0x3C
		if i+2 >= len(data) {
			result = append(result, base64Chars[current], '=')
			break
		}
		current |= (data[i+2] >> 6) & 0x03
		result = append(result, base64Chars[current])

		current = data[i+2] & 0x3F
		result = append(result, base64Chars[current])
	}
	return string(result)
}

func GetLoginKey(uid int) string {
	if privateKey == nil {
		return ""
	}
	getLoginKeyMu.Lock()
	defer getLoginKeyMu.Unlock()

	buff := BuildLoginKeyPlainHex(uid)
	hexBuff := Hex2Bin(buff)

	encrypted, err := rsa.SignPKCS1v15(nil, privateKey, 0, hexBuff)
	if err != nil {
		return ""
	}

	result := base64Encode(encrypted)
	return result
}

type AiUser struct {
	ID        int `json:"id"`
	Uid       int `json:"uid"`
	MsgState  int `json:"msg_state"`
	MoveState int `json:"move_state"`
}

type AiConfig struct {
	AIMsg     int
	AIZhanjie int
	AIYisu    int
	AISleep   int
}

type AiMapData struct {
	DituID          int
	FangjianID      int
	XMin            int
	XMax            int
	YMin            int
	YMax            int
	FangjianMaxTime int
}

type GuijiInfo struct {
	Index    int    `json:"index"`
	MaxIndex int    `json:"maxIndex"`
	Path     string `json:"path"`
}

type DnfTableTaskResult struct {
	Msg        string
	Code       int
	Uid        int
	OpenAllMsg []string
}

type TJRData struct {
	ID               int
	Uid              int
	MsgState         int
	MoveState        int
	UserYdsd         int
	UserMapIndex     int
	UserAIZhanjieEnd int
	UserAIXingzouEnd int
	UserEndMapTimeS  int
}

func USleepMicro(us int) {
	time.Sleep(time.Duration(us) * time.Microsecond)
}

func ReplaceAllDistinct(str, from, to string) string {
	result := str
	for {
		idx := -1
		for i := 0; i < len(result); i++ {
			if i+len(from) <= len(result) && result[i:i+len(from)] == from {
				idx = i
				break
			}
		}
		if idx == -1 {
			break
		}
		result = result[:idx] + to + result[idx+len(from):]
	}
	return result
}

func EncryptStr(str string, shift, decrft int) string {
	result := []byte(str)
	for i, c := range result {
		if c >= 'A' && c <= 'Z' {
			result[i] = byte('A' + (int(c-'A')+shift)%26)
		} else if c >= 'a' && c <= 'z' {
			result[i] = byte('a' + (int(c-'a')+shift)%26)
		} else if c >= '0' && c <= '9' {
			result[i] = byte('0' + (int(c-'0')+decrft)%10)
		} else {
			switch c {
			case '*':
				result[i] = '.'
			case '.':
				result[i] = '*'
			case '?':
				result[i] = '/'
			case '/':
				result[i] = '?'
			case '|':
				result[i] = ':'
			case ':':
				result[i] = '|'
			case '_':
				result[i] = '^'
			case '^':
				result[i] = '_'
			case '%':
				result[i] = '#'
			case '#':
				result[i] = '%'
			case '-':
				result[i] = '$'
			case '$':
				result[i] = '-'
			case ' ':
				result[i] = '='
			case '=':
				result[i] = ' '
			}
		}
	}
	return string(result)
}

func DecryptStr(str string, shift int) string {
	return EncryptStr(str, 26-shift, 10-shift)
}

type UserStatusItem struct {
	ID  int `json:"id"`
	Uid int `json:"uid"`
}

type MsgListResponse struct {
	Userstatus []UserStatusItem `json:"userstatus"`
}

func ParseMsgListResponse(data string) (*MsgListResponse, error) {
	var resp MsgListResponse
	if err := json.Unmarshal([]byte(data), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func MapIntStringToJSON(data map[int]map[string]int) []byte {
	type entry struct {
		Uid       int `json:"uid"`
		ID        int `json:"id"`
		MsgState  int `json:"msg_state"`
		MoveState int `json:"move_state"`
	}
	var entries []entry
	for uid, m := range data {
		entries = append(entries, entry{
			Uid:       uid,
			ID:        m["id"],
			MsgState:  m["msg_state"],
			MoveState: m["move_state"],
		})
	}
	b, _ := json.Marshal(entries)
	return b
}
