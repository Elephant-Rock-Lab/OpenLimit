package lifecycle

import (
	"sync"
	"sync/atomic"
)

type Tracker struct {
	inFlight     int64
	shuttingDown atomic.Bool
	wg           sync.WaitGroup
}

func NewTracker() *Tracker {
	return &Tracker{}
}

func (t *Tracker) Begin() func() {
	if t == nil {
		return func() {}
	}
	atomic.AddInt64(&t.inFlight, 1)
	t.wg.Add(1)
	return func() {
		atomic.AddInt64(&t.inFlight, -1)
		t.wg.Done()
	}
}

func (t *Tracker) InFlight() int64 {
	if t == nil {
		return 0
	}
	return atomic.LoadInt64(&t.inFlight)
}

func (t *Tracker) MarkShuttingDown() {
	if t != nil {
		t.shuttingDown.Store(true)
	}
}

func (t *Tracker) IsShuttingDown() bool {
	if t == nil {
		return false
	}
	return t.shuttingDown.Load()
}

func (t *Tracker) Wait() {
	if t != nil {
		t.wg.Wait()
	}
}
