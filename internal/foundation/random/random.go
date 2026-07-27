package random

import (
	mathrand "math/rand"
)

func BetweenAtLeast(r *mathrand.Rand, min, max int) int {
	if max < min {
		max = min
	}
	if r == nil {
		return min
	}
	return min + r.Intn(max-min+1)
}
