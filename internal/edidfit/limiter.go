package edidfit

import (
	"strings"
	"sync"
	"time"
)

// ChangeLimiter spaces out EDID changes: every change is a hotplug for the
// host. After a change, requests within Interval are held and only the last
// one is applied once the interval has passed. Changes are applied one at a
// time, and a request for the EDID already set does nothing.
type ChangeLimiter struct {
	Interval time.Duration
	Apply    func(edid string) error // sets the EDID now
	Current  func() string           // EDID currently set
	OnError  func(err error)         // a held change failed

	mu          sync.Mutex
	lastApplied time.Time
	pending     *string
	timer       *time.Timer
}

// Request applies edid now, or holds it until Interval after the last
// change. It returns how long until a held change is applied (0 when applied
// now or nothing to do).
func (l *ChangeLimiter) Request(edid string) (time.Duration, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.pending == nil && strings.EqualFold(edid, l.Current()) {
		return 0, nil
	}
	if wait := time.Until(l.lastApplied.Add(l.Interval)); wait > 0 {
		l.pending = &edid // the last request wins
		if l.timer == nil {
			l.timer = time.AfterFunc(wait, l.applyPending)
		}
		return wait, nil
	}
	if err := l.Apply(edid); err != nil {
		return 0, err
	}
	l.lastApplied = time.Now()
	return 0, nil
}

func (l *ChangeLimiter) applyPending() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.timer = nil
	if l.pending == nil {
		return
	}
	edid := *l.pending
	l.pending = nil
	if strings.EqualFold(edid, l.Current()) {
		return
	}
	if err := l.Apply(edid); err != nil {
		if l.OnError != nil {
			l.OnError(err)
		}
		return
	}
	l.lastApplied = time.Now()
}
