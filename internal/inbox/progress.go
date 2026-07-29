package inbox

import (
	"time"

	"github.com/1etu/ferry/internal/store"
)

func (in *Inbox) Progress(transferID string) (done int64, ok bool) {
	in.mu.Lock()
	defer in.mu.Unlock()
	u, ok := in.uploads[transferID]
	if !ok {
		return 0, false
	}
	return u.done, true
}

func (in *Inbox) drainProgress() {
	defer close(in.drained)
	for {
		select {
		case <-in.stopDrain:
			return
		case hook := <-in.tus.UploadProgress:
			in.advance(hook.Upload.ID, hook.Upload.Offset)
		}
	}
}

func (in *Inbox) advance(transferID string, offset int64) {
	in.mu.Lock()
	defer in.mu.Unlock()
	u, ok := in.uploads[transferID]
	if !ok || !u.isAnnounced {
		return
	}
	now := time.Now()
	if now.Sub(u.publishedAt) < publishInterval {
		return
	}
	u.publishedAt = now
	snapshot := u.transfer
	snapshot.Done = min(max(u.done, offset), u.transfer.Size)
	snapshot.UpdatedAt = now
	in.publish(snapshot)
}

func (in *Inbox) track(t store.Transfer) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.uploads[t.ID] = &upload{transfer: t, done: t.Done, isAnnounced: true}
}

func (in *Inbox) trackCreation(t store.Transfer) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.uploads[t.ID] = &upload{transfer: t, done: t.Done}
}

func (in *Inbox) announce(transferID string) {
	in.mu.Lock()
	u, ok := in.uploads[transferID]
	if !ok || u.isAnnounced {
		in.mu.Unlock()
		return
	}
	u.isAnnounced = true
	snapshot := u.transfer
	snapshot.Done = u.done
	snapshot.UpdatedAt = time.Now()
	in.mu.Unlock()
	in.publish(snapshot)
}

func (in *Inbox) untrack(transferID string) (store.Transfer, bool) {
	in.mu.Lock()
	defer in.mu.Unlock()
	u, ok := in.uploads[transferID]
	if !ok {
		return store.Transfer{}, false
	}
	delete(in.uploads, transferID)
	t := u.transfer
	t.Done = u.done
	return t, true
}

func (in *Inbox) tracked(transferID string) (store.Transfer, bool) {
	in.mu.Lock()
	defer in.mu.Unlock()
	u, ok := in.uploads[transferID]
	if !ok {
		return store.Transfer{}, false
	}
	t := u.transfer
	t.Done = u.done
	return t, true
}

func (in *Inbox) trackedOf(deviceID string) []string {
	in.mu.Lock()
	defer in.mu.Unlock()
	var ids []string
	for id, u := range in.uploads {
		if u.transfer.DeviceID == deviceID {
			ids = append(ids, id)
		}
	}
	return ids
}

func (in *Inbox) isOwnedBy(transferID, deviceID string) bool {
	t, ok := in.tracked(transferID)
	return ok && t.DeviceID == deviceID
}

func (in *Inbox) isComplete(transferID string, dataSize int64) bool {
	t, ok := in.tracked(transferID)
	return ok && dataSize >= t.Size
}

func (in *Inbox) outstanding() int64 {
	in.mu.Lock()
	defer in.mu.Unlock()
	var total int64
	for _, u := range in.uploads {
		total += max(0, u.transfer.Size-u.done)
	}
	return total
}

func (in *Inbox) beginWrite(deviceID string) bool {
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.writes[deviceID] >= in.cfg.MaxActivePerDevice {
		return false
	}
	in.writes[deviceID]++
	return true
}

func (in *Inbox) endWrite(deviceID string) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.writes[deviceID]--
	if in.writes[deviceID] == 0 {
		delete(in.writes, deviceID)
	}
}

func (in *Inbox) writesOf(deviceID string) int {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.writes[deviceID]
}
