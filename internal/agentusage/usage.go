// Package agentusage reads the token usage agent CLIs record in their own
// session files, and sums it per agent over the last days. It reads only
// counts, models and times: never prompts, output or file names.
package agentusage

import (
	"cmp"
	"errors"
	"io/fs"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/berth-ai/berth/internal/agentset"
	"github.com/berth-ai/berth/internal/locksource"
)

// ReportSchema is the schema of berth usage --json.
const ReportSchema = 1

// Days is how many calendar days a report covers, today included.
const Days = 7

// Tokens counts one kind of token each. CacheWrite is input written to the
// prompt cache; CacheRead is input served from it.
type Tokens struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
}

func (t Tokens) total() int64 { return t.Input + t.Output + t.CacheRead + t.CacheWrite }

func (t *Tokens) add(o Tokens) {
	t.Input += o.Input
	t.Output += o.Output
	t.CacheRead += o.CacheRead
	t.CacheWrite += o.CacheWrite
}

// Period is the usage of a span of days.
type Period struct {
	Tokens
	Total    int64 `json:"total"`
	Messages int   `json:"messages"` // model responses
	Sessions int   `json:"sessions"` // sessions with a response in the span
}

// Day is one calendar day's total, in the reader's time zone.
type Day struct {
	Date  string `json:"date"` // YYYY-MM-DD
	Total int64  `json:"total"`
}

// Model is one model's usage over the whole report.
type Model struct {
	Model string `json:"model"`
	Tokens
	Total    int64 `json:"total"`
	Messages int   `json:"messages"`
}

// Agent is one installed agent in berth usage.
type Agent struct {
	Name string `json:"name"`
	// Tracked is false when Berth cannot read this agent's usage: its lock
	// names no usage source, or one this build does not know.
	Tracked    bool       `json:"tracked"`
	Today      Period     `json:"today"`
	Week       Period     `json:"week"`
	Days       []Day      `json:"days"`   // oldest first, Days long
	Models     []Model    `json:"models"` // the most used first
	LastActive *time.Time `json:"last_active"`
	Error      string     `json:"error"` // why the files could not be read; the sums cover what could
}

// Report is berth usage --json.
type Report struct {
	Schema int              `json:"schema"`
	Since  time.Time        `json:"since"` // midnight, Days-1 days ago
	Agents map[string]Agent `json:"agents"`
}

// record is one model response.
type record struct {
	at      time.Time
	session string
	model   string
	tokens  Tokens
}

// reader calls add for each response in dir that may fall on or after
// since. A missing dir is no error: the agent has not run yet.
type reader func(dir string, since time.Time, add func(record)) error

// readers are the usage formats this build knows, by descriptor name.
var readers = map[string]reader{
	"claude-code": readClaudeCode,
}

// Env locates the agents and the agent user's home.
type Env struct {
	Set  agentset.Set
	Dirs locksource.HomeDirs
	Now  time.Time      // zero means time.Now()
	Loc  *time.Location // the days' time zone; nil means time.Local
}

// Report sums the usage of every installed agent, in parallel.
func (e Env) Report() Report {
	loc := cmp.Or(e.Loc, time.Local)
	now := e.Now
	if now.IsZero() {
		now = time.Now()
	}
	now = now.In(loc)
	y, m, d := now.Date()
	since := time.Date(y, m, d-(Days-1), 0, 0, 0, 0, loc)
	rep := Report{Schema: ReportSchema, Since: since, Agents: map[string]Agent{}}
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	for _, dep := range slices.Sorted(maps.Keys(e.Set.Packages)) {
		p := e.Set.Packages[dep]
		if p.Meta.Kind != locksource.KindAgent {
			continue
		}
		src := e.Set.Descriptors.Agents[dep].Usage
		wg.Go(func() {
			a := summarize(p.Meta.Name, src, e.Dirs, since, now)
			mu.Lock()
			rep.Agents[dep] = a
			mu.Unlock()
		})
	}
	wg.Wait()
	return rep
}

func summarize(name string, src *locksource.UsageSource, dirs locksource.HomeDirs, since, now time.Time) Agent {
	a := Agent{Name: name, Days: make([]Day, Days), Models: []Model{}}
	for i := range a.Days {
		a.Days[i].Date = since.AddDate(0, 0, i).Format(time.DateOnly)
	}
	var read reader
	if src != nil {
		read = readers[src.Format]
	}
	if read == nil {
		return a
	}
	a.Tracked = true
	loc := since.Location()
	todayStart := since.AddDate(0, 0, Days-1)
	models := map[string]*Model{}
	weekSessions, todaySessions := map[string]bool{}, map[string]bool{}
	err := read(dirs.Expand(src.Dir), since, func(r record) {
		if r.at.Before(since) || r.at.After(now) {
			return
		}
		at := r.at.In(loc)
		y, m, d := at.Date()
		day := int(time.Date(y, m, d, 0, 0, 0, 0, loc).Sub(since).Hours()+12) / 24
		if day < 0 || day >= Days {
			return
		}
		total := r.tokens.total()
		a.Days[day].Total += total
		a.Week.add(r.tokens)
		a.Week.Messages++
		weekSessions[r.session] = true
		if !at.Before(todayStart) {
			a.Today.add(r.tokens)
			a.Today.Messages++
			todaySessions[r.session] = true
		}
		md := models[r.model]
		if md == nil {
			md = &Model{Model: r.model}
			models[r.model] = md
		}
		md.add(r.tokens)
		md.Messages++
		if a.LastActive == nil || at.After(*a.LastActive) {
			t := at
			a.LastActive = &t
		}
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		a.Error = err.Error()
	}
	a.Week.Total, a.Week.Sessions = a.Week.total(), len(weekSessions)
	a.Today.Total, a.Today.Sessions = a.Today.total(), len(todaySessions)
	for _, md := range models {
		md.Total = md.total()
		a.Models = append(a.Models, *md)
	}
	slices.SortFunc(a.Models, func(x, y Model) int {
		return cmp.Or(cmp.Compare(y.Total, x.Total), cmp.Compare(x.Model, y.Model))
	})
	return a
}
