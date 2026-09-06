package common

import (
	"github.com/bytedance/gopkg/util/gopool"
	"sync"
)

// Enabled at startup only. A drain is irreversible within a process.
var ReleaseDrainEnabled bool
var ReleaseDrain = &DrainTracker{}

type DrainTracker struct {
	mu       sync.Mutex
	draining bool
	active   int
	failed   bool
}

type DrainSnapshot struct {
	Draining bool `json:"draining"`
	Active   int  `json:"active_work"`
	Failed   bool `json:"failed"`
}

func (d *DrainTracker) Start() (func(), bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.draining {
		return nil, false
	}
	d.active++
	return d.done, true
}

func (d *DrainTracker) Child() func() {
	d.mu.Lock()
	d.active++
	d.mu.Unlock()
	return d.done
}

func (d *DrainTracker) done()  { d.mu.Lock(); d.active--; d.mu.Unlock() }
func (d *DrainTracker) Begin() { d.mu.Lock(); d.draining = true; d.mu.Unlock() }
func (d *DrainTracker) Fail()  { d.mu.Lock(); d.failed = true; d.draining = true; d.mu.Unlock() }
func (d *DrainTracker) Snapshot() DrainSnapshot {
	d.mu.Lock()
	defer d.mu.Unlock()
	return DrainSnapshot{d.draining, d.active, d.failed}
}

func StartReleaseWork() (func(), bool) {
	if !ReleaseDrainEnabled {
		return func() {}, true
	}
	return ReleaseDrain.Start()
}

// Register before enqueueing, including children spawned during draining.
func BillingGo(fn func()) {
	if !ReleaseDrainEnabled {
		gopool.Go(fn)
		return
	}
	done := ReleaseDrain.Child()
	gopool.Go(func() {
		defer done()
		defer func() {
			if p := recover(); p != nil {
				ReleaseDrain.Fail()
				panic(p)
			}
		}()
		fn()
	})
}

func ReleaseWriteFailed() {
	if ReleaseDrainEnabled {
		ReleaseDrain.Fail()
	}
}
