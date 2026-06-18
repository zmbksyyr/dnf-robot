package service

import (
	"fmt"

	"robot/internal/dnf"
)

func robotLogf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Print(msg)
	dnf.LogString(dnf.LogLevelIndispensable, msg)
}
