package rsakey

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
)

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
