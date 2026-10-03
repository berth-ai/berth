package broker

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/berth-ai/bert/internal/config"
)

const (
	checkTTL          = time.Hour
	checkFetchTimeout = 30 * time.Second
)

// Check compares the current agent set with the lock on a ref.
// UpdateAvailable is nil when the ref could not be read.
type Check struct {
	Ref             string    `json:"ref"`
	Commit          string    `json:"commit,omitempty"`
	Hash            string    `json:"hash,omitempty"`
	UpdateAvailable *bool     `json:"update_available"`
	CheckedAt       time.Time `json:"checked_at"`
	Error           string    `json:"error,omitempty"`
}

type cachedLock struct {
	commit, hash string
	at           time.Time
}

func (b *Broker) updateCheck(ctx context.Context, body []byte) (int, any) {
	var in struct {
		Ref   string `json:"ref"`
		Force bool   `json:"force"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	cfg, _, _ := b.snapshot()
	ref := cmp.Or(in.Ref, cfg.Agents.Ref)
	if !config.ValidRef(ref) {
		return badRequest(fmt.Errorf("invalid ref %q", ref))
	}
	b.checkMu.Lock()
	cached, ok := b.checks[ref]
	b.checkMu.Unlock()
	now := b.now()
	if !ok || in.Force || now.Sub(cached.at) >= checkTTL {
		ctx, cancel := context.WithTimeout(ctx, checkFetchTimeout)
		defer cancel()
		lock, err := b.Fetch(ctx, ref)
		if err != nil {
			return http.StatusOK, map[string]any{"check": Check{Ref: ref, CheckedAt: now.UTC(), Error: err.Error()}}
		}
		cached = cachedLock{commit: lock.Source.Commit, hash: lock.Hash, at: now}
		b.checkMu.Lock()
		b.checks[ref] = cached
		b.checkMu.Unlock()
	}
	return http.StatusOK, map[string]any{"check": b.compare(ref, cached)}
}

// compare answers from a cached lock against the current set now, so a
// rollback after a check is reflected without fetching again.
func (b *Broker) compare(ref string, c cachedLock) Check {
	avail := b.currentHash() != c.hash
	return Check{Ref: ref, Commit: c.commit, Hash: c.hash, UpdateAvailable: &avail, CheckedAt: c.at.UTC()}
}

// cachedCheck is the last check of ref, without fetching; nil if none.
func (b *Broker) cachedCheck(ref string) *Check {
	b.checkMu.Lock()
	c, ok := b.checks[ref]
	b.checkMu.Unlock()
	if !ok {
		return nil
	}
	check := b.compare(ref, c)
	return &check
}

func (b *Broker) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

func (b *Broker) currentHash() string {
	if b.CurrentHash != nil {
		return b.CurrentHash()
	}
	if b.Layer != nil {
		if cur, err := b.Layer.Store.Current(); err == nil && cur != nil {
			return cur.Hash
		}
	}
	return ""
}
