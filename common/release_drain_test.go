package common

import (
	"sync"
	"testing"
)

func TestReleaseDrainAdmissionAndChildren(t *testing.T) {
	d := &DrainTracker{}
	done, ok := d.Start()
	if !ok {
		t.Fatal("initial request rejected")
	}
	d.Begin()
	if _, ok := d.Start(); ok {
		t.Fatal("admitted work after drain")
	}
	child := d.Child()
	done()
	if s := d.Snapshot(); s.Active != 1 || !s.Draining {
		t.Fatalf("lost child: %+v", s)
	}
	child()
	d.Fail()
	if s := d.Snapshot(); s.Active != 0 || !s.Failed {
		t.Fatalf("bad final state: %+v", s)
	}
}

func TestReleaseDrainFailureQuarantinesNewWork(t *testing.T) {
	d := &DrainTracker{}
	done, _ := d.Start()
	d.Fail()
	if _, ok := d.Start(); ok {
		t.Fatal("failed instance continued accumulating new work")
	}
	if d.Snapshot().Active != 1 {
		t.Fatal("in-flight request lost on failure")
	}
	done()
}

func TestReleaseDrainConcurrentAdmission(t *testing.T) {
	d := &DrainTracker{}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if done, ok := d.Start(); ok {
				child := d.Child()
				done()
				child()
			}
		}()
	}
	d.Begin()
	wg.Wait()
	if d.Snapshot().Active != 0 {
		t.Fatal("work leaked")
	}
	if _, ok := d.Start(); ok {
		t.Fatal("drain reopened")
	}
}
