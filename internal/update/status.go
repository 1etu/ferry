package update

import (
	"time"

	"github.com/1etu/ferry/internal/events"
)

type State string

const kindUpdate events.Kind = "update"

const (
	StateIdle        State = "idle"
	StateChecking    State = "checking"
	StateDownloading State = "downloading"
	StateReady       State = "ready"
	StateFailed      State = "failed"
	StateDisabled    State = "disabled"
)

type Status struct {
	Current   string    `json:"current"`
	Available string    `json:"available,omitempty"`
	State     State     `json:"state"`
	CheckedAt time.Time `json:"checkedAt,omitzero"`
	Error     string    `json:"error,omitempty"`
}

func (u *Updater) Status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.status
}

func (u *Updater) acquire() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	switch {
	case !u.canCheck:
		return ErrDisabled
	case u.isBusy:
		return ErrBusy
	}
	u.isBusy = true
	return nil
}

func (u *Updater) release() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.isBusy = false
}

func (u *Updater) set(mutate func(*Status)) Status {
	u.mu.Lock()
	mutate(&u.status)
	snapshot := u.status
	u.mu.Unlock()
	u.hub.Publish(events.Event{Kind: kindUpdate, OwnerOnly: true, Payload: snapshot})
	return snapshot
}

func (u *Updater) finish(state State, available string, err error) Status {
	return u.set(func(s *Status) {
		s.State, s.Available, s.Error = state, available, ""
		s.CheckedAt = u.now().UTC().Truncate(time.Second)
		if err != nil {
			s.Error = err.Error()
		}
	})
}

func (u *Updater) stagedVersion() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.staged
}

func (u *Updater) setPending(m Manifest) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.pending = &m
	u.staged = ""
}

func (u *Updater) pendingManifest() (Manifest, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.pending == nil {
		return Manifest{}, false
	}
	return *u.pending, true
}

func (u *Updater) markStaged(version string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.staged = version
}

func (u *Updater) clearStaged() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.pending = nil
	u.staged = ""
}
