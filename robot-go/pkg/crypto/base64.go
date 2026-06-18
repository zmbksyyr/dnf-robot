package crypto

import "encoding/base64"

var stdBase64 = base64.StdEncoding.WithPadding(base64.StdPadding)

func Base64Encode(data []byte) string {
	return stdBase64.EncodeToString(data)
}

func Base64Decode(s string) ([]byte, error) {
	return stdBase64.DecodeString(s)
}
