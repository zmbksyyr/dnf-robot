package random

import (
	"crypto/rand"
	"encoding/binary"
)

func GetIntRandom(min, max int) int {
	if min > max {
		min, max = max, min
	}

	var b [4]byte
	n, err := rand.Read(b[:])
	if err != nil || n < 4 {
		return min
	}

	val := binary.BigEndian.Uint32(b[:])
	return min + int(val%uint32(max-min+1))
}
