package crypto

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"math/big"
	"math/bits"
	"os"
)

var globalPrivateKey *rsa.PrivateKey

func swapByte32(v uint32) uint32 {
	return bits.ReverseBytes32(v)
}

func InitPrivateKey(pemPath string) (*rsa.PrivateKey, error) {
	keyData, err := os.ReadFile(pemPath)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(keyData)
	if block == nil {
		return nil, fmt.Errorf("failed to parse PEM block")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
	}

	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is not an RSA private key")
	}

	globalPrivateKey = rsaKey
	return rsaKey, nil
}

func ClosePrivateKey() {
	globalPrivateKey = nil
}

func RSAPrivateEncrypt(key *rsa.PrivateKey, data []byte) ([]byte, error) {
	// Raw RSA (no padding): data^d mod n
	k := key.Size()

	c := new(big.Int).SetBytes(data)
	m := new(big.Int).Exp(c, key.D, key.N)

	result := make([]byte, k)
	m.FillBytes(result)
	return result, nil
}

func GetLoginKey(uid uint32, key *rsa.PrivateKey) (string, error) {
	swapped := swapByte32(uid)

	// [C++->Go] Token鏍煎紡: 4瀛楄妭swappedUID + 32瀛楄妭0x01濉厖 + 4瀛楄妭鍥哄畾榄旀暟 + 6瀛楄妭鍚庣紑, 鍏?6瀛楄妭
	// C++: char srcToken[46] = {0,0,0,0, 0x01脳32, 0x55,0x91,0x45,0x10, 0x01,0x04,0x03,0x03,0x01,0x01}
	token := make([]byte, 46)

	// bytes 0-3: swapped UID
	binary.LittleEndian.PutUint32(token[0:4], swapped)

	// bytes 4-35: UID decimal string with \\0 padding (report.md fix, NOT 0x01)
	uidStr := fmt.Sprintf("%d", uid)
	copy(token[4:], []byte(uidStr))
	// rest of bytes 4-35 already zero from make()

	// bytes 36-39: fixed magic {0x55, 0x91, 0x45, 0x10}
	token[36] = 0x55
	token[37] = 0x91
	token[38] = 0x45
	token[39] = 0x10

	// bytes 40-45: fixed suffix {0x01, 0x04, 0x03, 0x03, 0x01, 0x01}
	token[40] = 0x01
	token[41] = 0x04
	token[42] = 0x03
	token[43] = 0x03
	token[44] = 0x01
	token[45] = 0x01

	// [C++->Go] OpenSSL RSA_private_encrypt with PKCS1 padding
	encrypted, err := RSAPrivateEncrypt(key, token)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(encrypted), nil
}
