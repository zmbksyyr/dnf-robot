package service

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"sync"
	"time"
)

var getLoginKeyMu sync.Mutex

var privateKey *rsa.PrivateKey

func InitPrivateKey(keyFile string) error {
	data, err := os.ReadFile(keyFile)
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
	result, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil
	}
	return result
}

func GetLoginKey(uid int) string {
	if privateKey == nil {
		return ""
	}
	getLoginKeyMu.Lock()
	defer getLoginKeyMu.Unlock()

	buff := BuildLoginKeyPlainHex(uid)
	hexBuff := Hex2Bin(buff)
	if hexBuff == nil {
		return ""
	}

	encrypted, err := rsa.SignPKCS1v15(nil, privateKey, 0, hexBuff)
	if err != nil {
		return ""
	}

	return base64.StdEncoding.EncodeToString(encrypted)
}
