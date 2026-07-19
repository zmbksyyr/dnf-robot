package rsakey

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"sync/atomic"
)

var privateKey atomic.Pointer[rsa.PrivateKey]

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
		parsed, ok := key2.(*rsa.PrivateKey)
		if !ok {
			return fmt.Errorf("key is not RSA")
		}
		privateKey.Store(parsed)
	} else {
		privateKey.Store(key)
	}
	return nil
}

func ClosePrivateKey() {
	privateKey.Store(nil)
}

func GetRSAKey() *rsa.PrivateKey {
	return privateKey.Load()
}
