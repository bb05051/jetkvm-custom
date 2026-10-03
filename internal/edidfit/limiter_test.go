package edidfit

import (
	"sync"
	"testing"
	"time"
)

type fakeDisplay struct {
	mu      sync.Mutex
	current string
	applied []string
}

func (f *fakeDisplay) apply(edid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.current = edid
	f.applied = append(f.applied, edid)
	return nil
}

func (f *fakeDisplay) get() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.current
}

func (f *fakeDisplay) history() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.applied...)
}

func TestChangeLimiter(t *testing.T) {
	d := &fakeDisplay{current: "AA"}
	l := &ChangeLimiter{Interval: 100 * time.Millisecond, Apply: d.apply, Current: d.get}

	// Same as current: nothing happens.
	if wait, _ := l.Request("aa"); wait != 0 || len(d.history()) != 0 {
		t.Fatalf("same EDID applied: wait %v, history %v", wait, d.history())
	}
	// First change applies right away.
	if wait, _ := l.Request("BB"); wait != 0 || d.get() != "BB" {
		t.Fatalf("first change: wait %v, current %s", wait, d.get())
	}
	// Changes within the interval are held; only the last one is applied.
	for _, e := range []string{"CC", "DD", "EE"} {
		if wait, _ := l.Request(e); wait <= 0 {
			t.Fatalf("%s not held", e)
		}
	}
	if d.get() != "BB" {
		t.Fatalf("held change applied early: %s", d.get())
	}
	waitFor(t, func() bool { return d.get() == "EE" })
	if got := d.history(); len(got) != 2 || got[1] != "EE" {
		t.Fatalf("history %v, want [BB EE]", got)
	}

	// Right after EE: FF is held, then replaced by a request for the current
	// EDID, so nothing changes when the interval ends.
	if wait, _ := l.Request("FF"); wait <= 0 {
		t.Fatal("FF not held")
	}
	l.Request("ee")
	time.Sleep(3 * l.Interval)
	if got := d.history(); len(got) != 2 {
		t.Fatalf("history %v, want no change back to the same EDID", got)
	}

	// After the interval a change applies right away again.
	if wait, _ := l.Request("GG"); wait != 0 || d.get() != "GG" {
		t.Fatalf("change after the interval: wait %v, current %s", wait, d.get())
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatal("condition not met in time")
}
