package mathx

func MinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func MaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func AbsInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func BoolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func IntersectRange(aMin, aMax, bMin, bMax int) (int, int, bool) {
	if aMax < aMin {
		aMin, aMax = aMax, aMin
	}
	if bMax < bMin {
		bMin, bMax = bMax, bMin
	}
	minV := MaxInt(aMin, bMin)
	maxV := MinInt(aMax, bMax)
	if maxV < minV {
		return 0, 0, false
	}
	return minV, maxV, true
}
