package scan

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestDebouncerFiresOnceAfterQuiet(t *testing.T) {
	var calls int32
	d := newDebouncer(50*time.Millisecond, func() { atomic.AddInt32(&calls, 1) })
	defer d.stop()
	// rapid triggers should collapse into a single call
	for i := 0; i < 5; i++ {
		d.trigger()
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(120 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 call, got %d", got)
	}
}

func TestDebouncerSeparateBurstsFireSeparately(t *testing.T) {
	var calls int32
	d := newDebouncer(40*time.Millisecond, func() { atomic.AddInt32(&calls, 1) })
	defer d.stop()
	d.trigger()
	time.Sleep(100 * time.Millisecond)
	d.trigger()
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 calls, got %d", got)
	}
}
