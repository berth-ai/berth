package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// serialJob runs one converger at a time. A change made while it runs
// queues one more run, so the last change always lands.
type serialJob struct {
	mu    sync.Mutex
	busy  bool // a run is going
	again bool // something changed during it
}

func (s *serialJob) start(jobs *Jobs, verb, group string, work Work, rerun string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		s.again = true
		return nil
	}
	_, err := jobs.Start(verb, group, nil, func(ctx context.Context, out io.Writer) (int, json.RawMessage, error) {
		for {
			code, res, err := work(ctx, out)
			s.mu.Lock()
			again := s.again && ctx.Err() == nil
			s.again, s.busy = false, again
			s.mu.Unlock()
			if !again {
				return code, res, err
			}
			fmt.Fprintln(out, rerun)
		}
	})
	if err != nil {
		return err
	}
	s.busy = true
	return nil
}
