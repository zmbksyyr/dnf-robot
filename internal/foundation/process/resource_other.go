//go:build !linux && !windows

package process

func processRSSMB() int { return 0 }

func processCPUPercent() float64 { return 0 }
