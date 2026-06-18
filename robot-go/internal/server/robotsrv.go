package server

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
)

type MsgType int

const (
	MsgError     MsgType = 6000
	MsgList      MsgType = 6001
	MsgCreate    MsgType = 6002
	MsgRemove    MsgType = 6003
	MsgLogin     MsgType = 6004
	MsgLogout    MsgType = 6005
	MsgMove      MsgType = 6006
	MsgPublicMsg MsgType = 6007
	MsgOnLine    MsgType = 6008
	MsgMaxCount  MsgType = 6009
)

type RobotMsg struct {
	MsgFlag byte
	FD      int
	MsgType MsgType
	JSON    json.RawMessage
}

type robotServer struct {
	mu         sync.Mutex
	msgQueue   []RobotMsg
	cond       *sync.Cond
	running    bool
	clientConn net.Conn
}

var instance *robotServer
var once sync.Once

func GetRobotServer() *robotServer {
	once.Do(func() {
		instance = &robotServer{}
		instance.cond = sync.NewCond(&instance.mu)
	})
	return instance
}

func (rs *robotServer) InitRobotServer() {
	rs.running = true
	go rs.workThread()
}

func (rs *robotServer) PushRobotMsg(msg RobotMsg) {
	rs.mu.Lock()
	if len(rs.msgQueue) >= 10000 {
		rs.mu.Unlock()
		fmt.Printf("[robotServer] msg_queue_full len=%d\n", 10000)
		return
	}
	rs.msgQueue = append(rs.msgQueue, msg)
	rs.mu.Unlock()
	rs.cond.Signal()
}

func (rs *robotServer) PopRobotMsg() (RobotMsg, bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if len(rs.msgQueue) == 0 {
		return RobotMsg{}, false
	}
	msg := rs.msgQueue[0]
	rs.msgQueue = rs.msgQueue[1:]
	return msg, true
}

func (rs *robotServer) QueueLen() int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return len(rs.msgQueue)
}

func (rs *robotServer) workThread() {
	for rs.running {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					fmt.Printf("[robotServer] workThread panic err=%v\n", rec)
				}
			}()
			rs.mu.Lock()
			for len(rs.msgQueue) == 0 && rs.running {
				rs.cond.Wait()
			}
			if !rs.running {
				rs.mu.Unlock()
				return
			}
			msgs := make([]RobotMsg, len(rs.msgQueue))
			copy(msgs, rs.msgQueue)
			rs.msgQueue = rs.msgQueue[:0]
			rs.mu.Unlock()

			for _, msg := range msgs {
				if rs.clientConn != nil {
					data, _ := json.Marshal(msg)
					rs.sendToServer(data)
				}
			}
		}()
	}
}

func (rs *robotServer) sendToServer(data []byte) {
	if rs.clientConn != nil {
		if _, err := rs.clientConn.Write(data); err != nil {
			fmt.Printf("[robotServer] write failed err=%v\n", err)
		}
	}
}
