package workspace

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"
)

// StatusBudget is how long one Status may take in all.
const StatusBudget = 5 * time.Second

// State is one workspace as the panel's list shows it.
type State struct {
	Exists   bool     `json:"exists"`
	Writable bool     `json:"writable"`
	Git      bool     `json:"git"`
	Branch   string   `json:"branch"` // empty when detached or not known
	Head     string   `json:"head"`   // short commit; empty when not known
	Dirty    int      `json:"dirty"`  // changed paths; -1 when not known
	Session  bool     `json:"session"`
	Windows  []string `json:"windows"`
	Unknown  bool     `json:"unknown"` // it did not answer within the budget
}

// Status reports on every workspace in dirs (id → absolute folder) within
// budget; one that has not answered by then is Unknown.
func Status(ctx context.Context, g Git, t Tmux, dirs map[string]string, budget time.Duration) map[string]State {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		out  = make(map[string]State, len(dirs))
		wins map[string][]string
		sem  = make(chan struct{}, 8)
	)
	wg.Go(func() {
		w, _ := t.Windows(ctx)
		mu.Lock()
		wins = w
		mu.Unlock()
	})
	for id, dir := range dirs {
		wg.Go(func() {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			st := folderState(ctx, g, dir)
			if ctx.Err() != nil {
				return
			}
			mu.Lock()
			out[id] = st
			mu.Unlock()
		})
	}
	wg.Wait()
	for id := range dirs {
		st, ok := out[id]
		if !ok {
			st = State{Unknown: true, Dirty: -1}
		}
		w := wins[SessionName(id)]
		st.Session, st.Windows = len(w) > 0, append([]string{}, w...)
		if st.Unknown {
			st.Session = false
		}
		out[id] = st
	}
	return out
}

func folderState(ctx context.Context, g Git, dir string) State {
	st := State{Dirty: -1}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return st
	}
	p := probe(dir)
	st.Exists, st.Writable, st.Git = true, p.Writable, p.Git
	if !st.Git {
		return st
	}
	st.Branch, _ = g.output(ctx, dir, "symbolic-ref", "--short", "-q", "HEAD")
	st.Head, _ = g.output(ctx, dir, "rev-parse", "--short=12", "HEAD")
	if out, err := g.output(ctx, dir, "status", "--porcelain", "--untracked-files=normal"); err == nil {
		st.Dirty = 0
		if out != "" {
			st.Dirty = strings.Count(out, "\n") + 1
		}
	}
	return st
}
