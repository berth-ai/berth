package broker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/berth-ai/berth/internal/pins"
	"github.com/berth-ai/berth/internal/status"
)

const (
	checkTTL     = time.Hour
	checkTimeout = 3 * time.Minute
)

// Check is the last look upstream: every pinned entity with the newest
// version there. Updates counts minor and patch updates, Majors the rest;
// UpdateAvailable is nil when the check failed.
type Check struct {
	CheckedAt       time.Time    `json:"checked_at"`
	Pins            []status.Pin `json:"pins"`
	Updates         int          `json:"updates"`
	Majors          int          `json:"majors"`
	UpdateAvailable *bool        `json:"update_available"`
	Error           string       `json:"error,omitempty"`
}

type cachedCheck struct {
	pins []status.Pin
	at   time.Time
}

func (b *Broker) updateCheck(ctx context.Context, body []byte) (int, any) {
	var in struct {
		Force bool `json:"force"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	b.checkMu.Lock()
	cached := b.lastCheck
	b.checkMu.Unlock()
	now := b.now()
	if cached == nil || in.Force || now.Sub(cached.at) >= checkTTL {
		got, err := b.checkPins(ctx)
		if err != nil {
			return http.StatusOK, map[string]any{"check": Check{CheckedAt: now.UTC(), Pins: []status.Pin{}, Error: err.Error()}}
		}
		cached = &cachedCheck{pins: got, at: now}
		b.checkMu.Lock()
		b.lastCheck = cached
		b.checkMu.Unlock()
	}
	return http.StatusOK, map[string]any{"check": b.compare(*cached)}
}

// compare answers from a cached check against what runs now, so an update
// or a rollback after a check is reflected without asking upstream again.
func (b *Broker) compare(c cachedCheck) Check {
	live := map[string]status.Pin{}
	if b.Agents != nil {
		if rep, err := b.Agents(); err == nil {
			for _, p := range rep.Pins {
				live[p.Name] = p
			}
		}
	}
	out := Check{CheckedAt: c.at.UTC(), Pins: make([]status.Pin, 0, len(c.pins))}
	for _, p := range c.pins {
		if l, ok := live[p.Name]; ok {
			p.Image, p.Current = l.Image, l.Current
		}
		p.Update = ""
		if p.Latest != "" {
			p.Update = pins.Bump(p.Current, p.Latest)
		}
		switch p.Update {
		case "":
		case pins.BumpMajor:
			out.Majors++
		default:
			out.Updates++
		}
		out.Pins = append(out.Pins, p)
	}
	avail := out.Updates+out.Majors > 0
	out.UpdateAvailable = &avail
	return out
}

// cachedCheckNow is the last check, without asking upstream; nil if none.
func (b *Broker) cachedCheckNow() *Check {
	b.checkMu.Lock()
	c := b.lastCheck
	b.checkMu.Unlock()
	if c == nil {
		return nil
	}
	check := b.compare(*c)
	return &check
}

// checkPins asks upstream through berth status --check, run as agent: the
// version lookups run mise, and nothing of that needs root.
func (b *Broker) checkPins(ctx context.Context) ([]status.Pin, error) {
	if b.CheckPins != nil {
		return b.CheckPins(ctx)
	}
	stdout, stderr, exit, err := b.agentRun(ctx, checkTimeout, b.Home.Berth, "status", "--check", "--json")
	if errors.Is(err, errTimeout) {
		return nil, errors.New("the check did not finish in time; try again")
	}
	if err != nil {
		return nil, err
	}
	var rep struct {
		Pins  []status.Pin `json:"pins"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if jerr := json.Unmarshal([]byte(stdout), &rep); jerr != nil || exit != 0 {
		if rep.Error != nil {
			return nil, errors.New(rep.Error.Message)
		}
		return nil, errors.New(strings.TrimSpace(lastLine(stderr)))
	}
	if rep.Pins == nil {
		rep.Pins = []status.Pin{}
	}
	return rep.Pins, nil
}

func (b *Broker) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}
