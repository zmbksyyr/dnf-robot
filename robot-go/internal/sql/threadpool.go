package sql

import (
	"sync"
)

type ThreadPool struct {
	jobQueue chan func()
	wg       sync.WaitGroup
	quit     chan struct{}
}

func NewThreadPool(numWorkers int) *ThreadPool {
	if numWorkers < 1 {
		numWorkers = 1
	}
	tp := &ThreadPool{
		jobQueue: make(chan func(), numWorkers*2),
		quit:     make(chan struct{}),
	}

	for i := 0; i < numWorkers; i++ {
		tp.wg.Add(1)
		go tp.worker()
	}

	return tp
}

func (tp *ThreadPool) worker() {
	defer tp.wg.Done()
	for {
		select {
		case fn, ok := <-tp.jobQueue:
			if !ok {
				return
			}
			if fn != nil {
				fn()
			}
		case <-tp.quit:
			return
		}
	}
}

func (tp *ThreadPool) AddWork(fn func()) {
	select {
	case tp.jobQueue <- fn:
	default:
		go fn()
	}
}

func (tp *ThreadPool) Close() {
	close(tp.quit)
	close(tp.jobQueue)
	tp.wg.Wait()
}
