package marketapp

func (a *App) randomFloat64() float64 {
	a.randMu.Lock()
	value := a.rand.Float64()
	a.randMu.Unlock()
	return value
}

func (a *App) randomIntn(n int) int {
	a.randMu.Lock()
	value := a.rand.Intn(n)
	a.randMu.Unlock()
	return value
}

func (a *App) randomInt63n(n int64) int64 {
	a.randMu.Lock()
	value := a.rand.Int63n(n)
	a.randMu.Unlock()
	return value
}

func (a *App) randomRange(min, max int) int {
	a.randMu.Lock()
	value := randRange(a.rand, min, max)
	a.randMu.Unlock()
	return value
}

func (a *App) randomShuffle(n int, swap func(i, j int)) {
	a.randMu.Lock()
	a.rand.Shuffle(n, swap)
	a.randMu.Unlock()
}
