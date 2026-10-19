package clientregistry

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
)

func TestConcurrentAccessHasNoRace(t *testing.T) {
	r := New()
	var wg sync.WaitGroup

	for g := 0; g < 200; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			id := fmt.Sprintf("inst-%d", g%20)
			for i := 0; i < 50; i++ {
				e := NewExecution(id, &whatsmeow.Client{}, nil)
				r.Register(e)
				_ = r.Client(id)
				_ = r.MyClient(id)
				_ = r.IsCurrent(e)
				_ = r.Count()
				if i%3 == 0 {
					r.StopCurrent(id, StopReconnect)
				}
				e.Stop(StopRemove)
				r.RemoveIf(e)
			}
		}(g)
	}
	wg.Wait()
}

func TestRemoveIfKeepsNewerExecution(t *testing.T) {
	r := New()
	old := NewExecution("a", &whatsmeow.Client{}, nil)
	newer := NewExecution("a", &whatsmeow.Client{}, nil)

	r.Register(old)
	r.Register(newer)

	if r.RemoveIf(old) {
		t.Fatal("RemoveIf of the old execution removed the entry")
	}
	if r.Client("a") != newer.Client() {
		t.Fatal("registry no longer points to the newer client")
	}
	if !r.RemoveIf(newer) {
		t.Fatal("RemoveIf of the current execution did not remove it")
	}
	if r.Client("a") != nil {
		t.Fatal("entry still present after RemoveIf of the current execution")
	}
}

func TestRegisterStopsPreviousExecution(t *testing.T) {
	r := New()
	old := NewExecution("a", &whatsmeow.Client{}, nil)
	r.Register(old)

	previous := r.Register(NewExecution("a", &whatsmeow.Client{}, nil))

	if previous != old {
		t.Fatal("Register did not return the previous execution")
	}
	select {
	case <-old.Done():
	default:
		t.Fatal("previous execution was left running")
	}
	if old.Reason() != StopReplaced {
		t.Fatalf("previous execution stopped with %q, want %q", old.Reason(), StopReplaced)
	}
}

func TestIsCurrentIsFalseForOldExecution(t *testing.T) {
	r := New()
	old := NewExecution("a", nil, nil)
	r.Register(old)
	if !r.IsCurrent(old) {
		t.Fatal("registered execution is not current")
	}

	r.Register(NewExecution("a", nil, nil))

	if r.IsCurrent(old) {
		t.Fatal("old execution still reported as current")
	}
}

func TestStopTwiceDoesNotPanicAndOnlyFirstWins(t *testing.T) {
	e := NewExecution("a", nil, nil)

	if !e.Stop(StopLogout) {
		t.Fatal("first Stop returned false")
	}
	if e.Stop(StopRemove) {
		t.Fatal("second Stop returned true")
	}
	if e.Reason() != StopLogout {
		t.Fatalf("reason = %q, want the first one (%q)", e.Reason(), StopLogout)
	}
}

func TestStopWakesEveryWaiter(t *testing.T) {
	e := NewExecution("a", nil, nil)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-e.Done()
		}()
	}

	e.Stop(StopRemove)

	woke := make(chan struct{})
	go func() { wg.Wait(); close(woke) }()
	select {
	case <-woke:
	case <-time.After(time.Second):
		t.Fatal("not every waiter woke up after Stop")
	}
}

func TestStopCurrentWithoutEntry(t *testing.T) {
	r := New()
	if r.StopCurrent("missing", StopRemove) {
		t.Fatal("StopCurrent returned true for an instance that is not registered")
	}
}

func TestStopCurrentStopsOnlyTheRegisteredOne(t *testing.T) {
	r := New()
	e := NewExecution("a", nil, nil)
	r.Register(e)

	if !r.StopCurrent("a", StopLogout) {
		t.Fatal("StopCurrent returned false for a running execution")
	}
	if e.Reason() != StopLogout {
		t.Fatalf("reason = %q, want %q", e.Reason(), StopLogout)
	}
	// StopCurrent only signals; the execution's own loop removes itself.
	if !r.IsCurrent(e) {
		t.Fatal("StopCurrent removed the entry; removal belongs to the execution")
	}
}

func TestCountTracksRegisteredExecutions(t *testing.T) {
	r := New()
	a := NewExecution("a", nil, nil)
	b := NewExecution("b", nil, nil)
	r.Register(a)
	r.Register(b)
	if r.Count() != 2 {
		t.Fatalf("Count = %d, want 2", r.Count())
	}
	r.RemoveIf(a)
	if r.Count() != 1 {
		t.Fatalf("Count = %d, want 1", r.Count())
	}
}

// Same shape as StartClient: register, wait on Done, remove itself. A hundred
// start/stop cycles of one instance must not leave goroutines behind — before,
// every reconnect left one loop alive (591 at the 16/10/2026 crash).
func TestStartStopCyclesLeaveNoGoroutines(t *testing.T) {
	r := New()
	baseline := runtime.NumGoroutine()

	for i := 0; i < 100; i++ {
		e := NewExecution("a", nil, nil)
		r.Register(e) // stops the previous cycle's execution
		go func() {
			<-e.Done()
			r.RemoveIf(e)
		}()
	}
	r.StopCurrent("a", StopRemove)

	deadline := time.Now().Add(2 * time.Second)
	for (runtime.NumGoroutine() > baseline+5 || r.Count() != 0) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > baseline+5 {
		t.Fatalf("goroutines = %d after 100 cycles, baseline %d", n, baseline)
	}
	if r.Count() != 0 {
		t.Fatalf("Count = %d after the last execution stopped, want 0", r.Count())
	}
}
