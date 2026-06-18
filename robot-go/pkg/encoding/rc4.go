package encoding

import "encoding/base64"

type RC4Ctx struct {
	s    [256]byte
	i, j byte
}

func RC4Init(key []byte) *RC4Ctx {
	ctx := &RC4Ctx{}
	var k [256]byte
	for i := 0; i < 256; i++ {
		ctx.s[i] = byte(i)
		k[i] = key[i%len(key)]
	}
	var j byte
	for i := 0; i < 256; i++ {
		j = j + ctx.s[i] + k[i]
		ctx.s[i], ctx.s[j] = ctx.s[j], ctx.s[i]
	}
	return ctx
}

func RC4Crypt(ctx *RC4Ctx, data []byte) []byte {
	result := make([]byte, len(data))
	i := ctx.i
	j := ctx.j
	for k := 0; k < len(data); k++ {
		i = i + 1
		j = j + ctx.s[i]
		ctx.s[i], ctx.s[j] = ctx.s[j], ctx.s[i]
		t := ctx.s[i] + ctx.s[j]
		result[k] = data[k] ^ ctx.s[t]
	}
	ctx.i = i
	ctx.j = j
	return result
}

func ZBase64Decode(encoded string, key string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}

	ctx := RC4Init([]byte(key))
	decrypted := RC4Crypt(ctx, raw)

	for i, j := 0, len(decrypted)-1; i < j; i, j = i+1, j-1 {
		decrypted[i], decrypted[j] = decrypted[j], decrypted[i]
	}

	for i := 0; i+1 < len(decrypted); i += 2 {
		decrypted[i], decrypted[i+1] = decrypted[i+1], decrypted[i]
	}

	return string(decrypted), nil
}
