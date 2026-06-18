package config

import (
	"strings"
	"sync"
)

func ParseMapObj(xml string) map[string]string {
	result := make(map[string]string)
	remainder := xml
	for {
		ltIdx := strings.Index(remainder, "<")
		if ltIdx == -1 {
			break
		}
		gtIdx := strings.Index(remainder[ltIdx:], ">")
		if gtIdx == -1 {
			break
		}
		key := remainder[ltIdx+1 : ltIdx+gtIdx]
		closeTag := "</" + key + ">"
		openTagEnd := ltIdx + gtIdx + 1
		closeIdx := strings.Index(remainder[openTagEnd:], closeTag)
		if closeIdx == -1 {
			remainder = remainder[openTagEnd:]
			continue
		}
		value := remainder[openTagEnd : openTagEnd+closeIdx]
		result[key] = value
		remainder = remainder[openTagEnd+closeIdx+len(closeTag):]
	}
	return result
}

func MapObjToString(m map[string]string) string {
	var sb strings.Builder
	for k, v := range m {
		sb.WriteString("<")
		sb.WriteString(k)
		sb.WriteString(">")
		sb.WriteString(v)
		sb.WriteString("</")
		sb.WriteString(k)
		sb.WriteString(">")
	}
	return sb.String()
}

type Task struct {
	Buffer   [1024]byte
	SocketFD int
	Type     int
}

type QueueInfo struct {
	Value Task
	Next  *QueueInfo
}

func CreateQueue() *QueueInfo {
	return &QueueInfo{}
}

func (q *QueueInfo) Push(value Task) {
	node := &QueueInfo{Value: value}
	cur := q
	for cur.Next != nil {
		cur = cur.Next
	}
	cur.Next = node
}

func (q *QueueInfo) Pop(value *Task) bool {
	if q.IsEmpty() {
		return false
	}
	*value = q.Next.Value
	q.Next = q.Next.Next
	return true
}

func (q *QueueInfo) Top(value *Task) bool {
	if q.IsEmpty() {
		return false
	}
	*value = q.Next.Value
	return true
}

func (q *QueueInfo) IsEmpty() bool {
	return q.Next == nil
}

type TaskQueue struct {
	mu    sync.Mutex
	queue *QueueInfo
}

func NewTaskQueue() *TaskQueue {
	return &TaskQueue{queue: CreateQueue()}
}

func (tq *TaskQueue) Lock()   { tq.mu.Lock() }
func (tq *TaskQueue) Unlock() { tq.mu.Unlock() }
func (tq *TaskQueue) Q() *QueueInfo {
	return tq.queue
}

func SplitString(s string, sep string) []string {
	return strings.Split(s, sep)
}

func ReadFileList(basePath string) ([]string, error) {
	// stub for filesystem listing
	return nil, nil
}
