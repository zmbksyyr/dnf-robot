package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	robotcap "robot/internal/capability/robot"
	"robot/internal/foundation/lockhub"
)

const (
	defaultPositionFlushDelay   = time.Second
	defaultPositionRetryMin     = 250 * time.Millisecond
	defaultPositionRetryMax     = 2 * time.Second
	defaultPositionWriteTimeout = 5 * time.Second
	defaultPositionCloseTimeout = 10 * time.Second
)

var errPositionBatcherClosed = errors.New("robot position batcher is closed")

type robotPositionWriter interface {
	UpdateRobotPositions(context.Context, []robotcap.PositionUpdate) error
}

type positionTimer interface {
	Stop() bool
}

type positionBatchOptions struct {
	flushDelay   time.Duration
	retryMin     time.Duration
	retryMax     time.Duration
	writeTimeout time.Duration
	closeTimeout time.Duration
	afterFunc    func(time.Duration, func()) positionTimer
}

func defaultPositionBatchOptions() positionBatchOptions {
	return positionBatchOptions{
		flushDelay:   defaultPositionFlushDelay,
		retryMin:     defaultPositionRetryMin,
		retryMax:     defaultPositionRetryMax,
		writeTimeout: defaultPositionWriteTimeout,
		closeTimeout: defaultPositionCloseTimeout,
		afterFunc: func(delay time.Duration, callback func()) positionTimer {
			return time.AfterFunc(delay, callback)
		},
	}
}

type positionBatcher struct {
	writer       robotPositionWriter
	flushDelay   time.Duration
	retryMin     time.Duration
	retryMax     time.Duration
	writeTimeout time.Duration
	closeTimeout time.Duration
	afterFunc    func(time.Duration, func()) positionTimer

	mu            lockhub.Locker
	pending       map[int]robotcap.PositionUpdate
	timer         positionTimer
	flushing      bool
	closed        bool
	retryDelay    time.Duration
	failureCount  int
	callbackCount int
	callbacksIdle chan struct{}
	closeOnce     sync.Once
	closeErr      error
}

func newPositionBatcher(writer robotPositionWriter, options positionBatchOptions) *positionBatcher {
	if writer == nil {
		writer = missingPositionRepository{}
	}
	defaults := defaultPositionBatchOptions()
	if options.flushDelay <= 0 {
		options.flushDelay = defaults.flushDelay
	}
	if options.retryMin <= 0 {
		options.retryMin = defaults.retryMin
	}
	if options.retryMax <= 0 {
		options.retryMax = defaults.retryMax
	}
	if options.retryMax < options.retryMin {
		options.retryMax = options.retryMin
	}
	if options.writeTimeout <= 0 {
		options.writeTimeout = defaults.writeTimeout
	}
	if options.closeTimeout <= 0 {
		options.closeTimeout = defaults.closeTimeout
	}
	if options.afterFunc == nil {
		options.afterFunc = defaults.afterFunc
	}
	return &positionBatcher{
		writer:       writer,
		flushDelay:   options.flushDelay,
		retryMin:     options.retryMin,
		retryMax:     options.retryMax,
		writeTimeout: options.writeTimeout,
		closeTimeout: options.closeTimeout,
		afterFunc:    options.afterFunc,
		pending:      make(map[int]robotcap.PositionUpdate),
	}
}

func (b *positionBatcher) Queue(info robotcap.Info, village, area, x, y int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return errPositionBatcherClosed
	}
	update := robotcap.PositionUpdate{
		UID:         info.UID,
		CID:         info.CID,
		FromVillage: info.Village,
		FromArea:    info.Area,
		FromX:       info.X,
		FromY:       info.Y,
		Village:     village,
		Area:        area,
		X:           x,
		Y:           y,
	}
	if previous, ok := b.pending[info.UID]; ok && positionUpdatesConnect(previous, update) {
		update.FromVillage = previous.FromVillage
		update.FromArea = previous.FromArea
		update.FromX = previous.FromX
		update.FromY = previous.FromY
	}
	b.pending[info.UID] = update
	b.scheduleLocked(b.flushDelay)
	return nil
}

func (b *positionBatcher) Close() error {
	b.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), b.closeTimeout)
		defer cancel()

		b.mu.Lock()
		b.closed = true
		if b.timer != nil {
			timer := b.timer
			b.timer = nil
			if timer.Stop() {
				b.finishCallbackLocked()
			}
		}
		callbacksIdle := b.callbacksIdle
		callbacksActive := b.callbackCount > 0
		b.mu.Unlock()

		if callbacksActive {
			select {
			case <-callbacksIdle:
			case <-ctx.Done():
				b.closeErr = fmt.Errorf("wait for in-flight position flush: %w", ctx.Err())
				return
			}
		}

		b.mu.Lock()
		batch := b.takePendingLocked()
		b.mu.Unlock()
		if len(batch) > 0 {
			if err := b.writePositions(ctx, batch); err != nil {
				b.mu.Lock()
				b.mergeFailedLocked(batch)
				b.mu.Unlock()
				b.closeErr = fmt.Errorf("flush pending robot positions during close: %w", err)
			}
		}
	})
	return b.closeErr
}

func (b *positionBatcher) scheduleLocked(delay time.Duration) {
	if b.closed || b.flushing || b.timer != nil || len(b.pending) == 0 {
		return
	}
	b.startCallbackLocked()
	b.timer = b.afterFunc(delay, func() {
		defer b.finishCallback()
		b.flushTimer()
	})
}

func (b *positionBatcher) flushTimer() {
	b.mu.Lock()
	b.timer = nil
	if b.closed {
		b.mu.Unlock()
		return
	}
	batch := b.takePendingLocked()
	if len(batch) == 0 {
		b.mu.Unlock()
		return
	}
	b.flushing = true
	b.mu.Unlock()

	err := b.writePositions(context.Background(), batch)

	b.mu.Lock()
	b.flushing = false
	logFailure := false
	logRecovery := false
	if err != nil {
		b.mergeFailedLocked(batch)
		b.failureCount++
		logFailure = b.failureCount == 1
		b.retryDelay = b.nextRetryDelayLocked()
		b.scheduleLocked(b.retryDelay)
	} else {
		b.rebasePendingAfterSuccessLocked(batch)
		logRecovery = b.failureCount > 0
		b.failureCount = 0
		b.retryDelay = 0
		b.scheduleLocked(b.flushDelay)
	}
	b.mu.Unlock()

	if logFailure {
		robotLogf("[PositionBatch] flush_failed count=%d err=%v\n", len(batch), err)
	}
	if logRecovery {
		robotLogf("[PositionBatch] flush_recovered count=%d\n", len(batch))
	}
}

func (b *positionBatcher) writePositions(parent context.Context, batch []robotcap.PositionUpdate) error {
	if len(batch) == 0 {
		return nil
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, b.writeTimeout)
	defer cancel()
	return b.writer.UpdateRobotPositions(ctx, batch)
}

func (b *positionBatcher) startCallbackLocked() {
	if b.callbackCount == 0 {
		b.callbacksIdle = make(chan struct{})
	}
	b.callbackCount++
}

func (b *positionBatcher) finishCallback() {
	b.mu.Lock()
	b.finishCallbackLocked()
	b.mu.Unlock()
}

func (b *positionBatcher) finishCallbackLocked() {
	if b.callbackCount <= 0 {
		return
	}
	b.callbackCount--
	if b.callbackCount == 0 {
		close(b.callbacksIdle)
	}
}

func (b *positionBatcher) takePendingLocked() []robotcap.PositionUpdate {
	if len(b.pending) == 0 {
		return nil
	}
	batch := make([]robotcap.PositionUpdate, 0, len(b.pending))
	for _, update := range b.pending {
		batch = append(batch, update)
	}
	sort.Slice(batch, func(i, j int) bool {
		return batch[i].UID < batch[j].UID
	})
	b.pending = make(map[int]robotcap.PositionUpdate)
	return batch
}

func (b *positionBatcher) mergeFailedLocked(batch []robotcap.PositionUpdate) {
	for _, failed := range batch {
		newer, hasNewer := b.pending[failed.UID]
		if !hasNewer {
			b.pending[failed.UID] = failed
			continue
		}
		if positionUpdatesConnect(failed, newer) {
			newer.FromVillage = failed.FromVillage
			newer.FromArea = failed.FromArea
			newer.FromX = failed.FromX
			newer.FromY = failed.FromY
			b.pending[failed.UID] = newer
		}
	}
}

func (b *positionBatcher) rebasePendingAfterSuccessLocked(batch []robotcap.PositionUpdate) {
	for _, committed := range batch {
		newer, ok := b.pending[committed.UID]
		if !ok || !positionUpdatesShareSource(committed, newer) {
			continue
		}
		newer.FromVillage = committed.Village
		newer.FromArea = committed.Area
		newer.FromX = committed.X
		newer.FromY = committed.Y
		b.pending[committed.UID] = newer
	}
}

func positionUpdatesShareSource(first, second robotcap.PositionUpdate) bool {
	return first.UID == second.UID &&
		first.CID == second.CID &&
		first.FromVillage == second.FromVillage &&
		first.FromArea == second.FromArea &&
		first.FromX == second.FromX &&
		first.FromY == second.FromY
}

func positionUpdatesConnect(earlier, later robotcap.PositionUpdate) bool {
	return earlier.UID == later.UID &&
		earlier.CID == later.CID &&
		earlier.Village == later.FromVillage &&
		earlier.Area == later.FromArea &&
		earlier.X == later.FromX &&
		earlier.Y == later.FromY
}

func (b *positionBatcher) nextRetryDelayLocked() time.Duration {
	if b.retryDelay <= 0 {
		return b.retryMin
	}
	next := b.retryDelay * 2
	if next > b.retryMax {
		return b.retryMax
	}
	return next
}
