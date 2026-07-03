package log

import "fmt"

type Sink func(msg string)

var robotSink Sink

func SetRobotSink(sink Sink) {
	robotSink = sink
}

func Robotf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Print(msg)
	if robotSink != nil {
		robotSink(msg)
	}
}
