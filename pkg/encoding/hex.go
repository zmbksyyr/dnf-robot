package encoding

import "encoding/hex"

func Hex2Bin(hexStr string) ([]byte, error) {
	return hex.DecodeString(hexStr)
}

func Bin2Hex(data []byte) string {
	return hex.EncodeToString(data)
}
