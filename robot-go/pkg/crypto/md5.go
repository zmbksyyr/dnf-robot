package crypto

import (
	"crypto/md5"
	"encoding/hex"
)

func CalMD5(data []byte) string {
	hash := md5.Sum(data)
	return hex.EncodeToString(hash[:])
}
