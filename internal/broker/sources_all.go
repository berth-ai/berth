package broker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/berth-ai/berth/internal/config"
)

// maxBulkSources bounds one skills.sources.update_all.
const maxBulkSources = 256

// sourceCheck is one source in skills.sources.check's answer: what moving
// it to the newest commit of its ref changes, or why the scan failed.
type sourceCheck struct {
	sourceDiff
	Error *fieldError `json:"error,omitempty"`
}

// skillsSourcesCheck scans every source at its ref, one after another in
// one job, and answers each one's preview. It changes nothing.
func (b *Broker) skillsSourcesCheck(_ context.Context, body []byte) (int, any) {
	var in struct{}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if code, v, ok := b.normalMode(); !ok {
		return code, v
	}
	cur, _, _ := b.snapshot()
	sources := cur.Skills.Sources
	shown := []string{b.Home.Berth, "skills", "scan", "--json"}
	return b.bulkJob("skills.sources.check", shown, func(ctx context.Context, out io.Writer) (int, json.RawMessage, error) {
		res := []sourceCheck{}
		for _, id := range slices.Sorted(maps.Keys(sources)) {
			src := sources[id]
			fmt.Fprintf(out, "check     %s: %s at %s\n", id, src.Repo, src.Ref)
			s, err := b.scanNow(ctx, out, src.Repo, src.Ref, src.Path)
			if err != nil {
				fmt.Fprintf(out, "error: %s: %v\n", id, err)
				res = append(res, sourceCheck{sourceDiff: emptyDiff(id, src), Error: asFieldError(err)})
				continue
			}
			res = append(res, sourceCheck{sourceDiff: previewDiff(id, src, src.Ref, s)})
		}
		raw, err := json.Marshal(map[string]any{"sources": res})
		return 0, raw, err
	})
}

// emptyDiff is a source's diff with no changes, for a failed check.
func emptyDiff(id string, src config.Source) sourceDiff {
	return sourceDiff{ID: id, Ref: src.Ref, From: src.Commit, Commit: src.Commit,
		Added: []diffRow{}, Removed: []diffRow{}, Moved: []diffRow{}, Described: []diffRow{}}
}

func asFieldError(err error) *fieldError {
	var fe *fieldError
	if errors.As(err, &fe) {
		return fe
	}
	return &fieldError{Code: "scan_failed", Message: err.Error()}
}

// bulkUpdate is one source a skills.sources.update_all moves: the ref and
// commit its check reported.
type bulkUpdate struct {
	ID     string `json:"id"`
	Ref    string `json:"ref"`
	Commit string `json:"commit"`
}

// bulkFailure is a source skills.sources.update_all left as it was.
type bulkFailure struct {
	ID      string `json:"id"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// skillsSourcesUpdateAll moves each listed source to the commit its check
// reported, in one job and one config change, so the skills sync runs
// once. A source that fails to scan, or that was removed or changed since
// the check, stays as it was; the others still move, and the job fails
// naming them.
func (b *Broker) skillsSourcesUpdateAll(_ context.Context, body []byte) (int, any) {
	var in struct {
		Updates []bulkUpdate `json:"updates"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if code, v, ok := b.normalMode(); !ok {
		return code, v
	}
	if len(in.Updates) == 0 || len(in.Updates) > maxBulkSources {
		return rejectJob(&fieldError{Code: "invalid_request", Field: "updates",
			Message: fmt.Sprintf("List 1 to %d sources to update", maxBulkSources)})
	}
	cur, _, _ := b.snapshot()
	srcs := map[string]config.Source{}
	for _, u := range in.Updates {
		src, ok := cur.Skills.Sources[u.ID]
		switch _, dup := srcs[u.ID]; {
		case !ok:
			return fail(http.StatusNotFound, "unknown_source", "No source named %s", u.ID)
		case dup:
			return rejectJob(&fieldError{Code: "invalid_request", Field: "updates", Message: "List each source once"})
		case !config.ValidCommit(u.Commit):
			return rejectJob(&fieldError{Code: "invalid_request", Field: "commit", Message: "Check for updates again; a commit is missing"})
		case u.Ref != "" && u.Ref != src.Ref:
			return rejectJob(&fieldError{Code: "source_changed", Field: "ref",
				Message: fmt.Sprintf("The ref of %s changed since the check; check for updates again", u.ID)})
		}
		srcs[u.ID] = src
	}
	shown := []string{b.Home.Berth, "skills", "scan", "--json"}
	return b.bulkJob("skills.sources.update_all", shown, func(ctx context.Context, out io.Writer) (int, json.RawMessage, error) {
		failed := []bulkFailure{}
		failOf := func(id string, err error) {
			fe := asFieldError(err)
			fmt.Fprintf(out, "error: %s: %s\n", id, fe.Message)
			failed = append(failed, bulkFailure{ID: id, Code: fe.Code, Message: fe.Message})
		}
		scans := map[string]skillScan{}
		for _, u := range in.Updates {
			src := srcs[u.ID]
			fmt.Fprintf(out, "update    %s: %s at %s\n", u.ID, src.Ref, u.Commit)
			s, err := b.scanNow(ctx, out, src.Repo, u.Commit, src.Path)
			if err != nil {
				failOf(u.ID, err)
				continue
			}
			scans[u.ID] = s
		}
		type moved struct {
			ID      string   `json:"id"`
			From    string   `json:"from"`
			Commit  string   `json:"commit"`
			Missing []string `json:"missing"`
		}
		updated, unchanged := []moved{}, []string{}
		next, changed, err := b.applyPatch(func(cur config.Config) (map[string]any, error) {
			patch := map[string]any{}
			for _, u := range in.Updates {
				s, ok := scans[u.ID]
				if !ok {
					continue
				}
				src := srcs[u.ID]
				if now := cur.Skills.Sources[u.ID]; now.Ref != src.Ref {
					failOf(u.ID, &fieldError{Code: "unknown_source", Message: fmt.Sprintf("The source %s was removed or changed meanwhile; try again", u.ID)})
					continue
				}
				p, missing, err := sourceUpdate(cur, u.ID, src, src.Ref, s)
				switch {
				case err != nil:
					failOf(u.ID, err)
				case p == nil:
					unchanged = append(unchanged, u.ID)
				default:
					patch[u.ID] = p
					updated = append(updated, moved{ID: u.ID, From: cur.Skills.Sources[u.ID].Commit, Commit: s.Commit, Missing: missing})
				}
			}
			if len(patch) == 0 {
				return nil, nil
			}
			return map[string]any{"skills": map[string]any{"sources": patch}}, nil
		})
		if err != nil {
			fmt.Fprintf(out, "error: %v\n", err)
			return 1, fieldFailure(err), nil
		}
		res := map[string]any{"updated": updated, "unchanged": unchanged, "failed": failed,
			"changed": changed, "revision": next.Revision}
		code := 0
		if len(failed) > 0 {
			code = 1
			ids := make([]string, len(failed))
			for i, f := range failed {
				ids[i] = f.ID
			}
			res["error"] = &fieldError{Code: "update_failed", Field: "updates",
				Message: fmt.Sprintf("Could not update %s; see the log", strings.Join(ids, ", "))}
		}
		raw, err := json.Marshal(res)
		return code, raw, err
	})
}

// bulkJob starts work as job verb in the skills group.
func (b *Broker) bulkJob(verb string, shown []string, work Work) (int, any) {
	job, err := b.Jobs.Start(verb, skillsGroup, shown, work)
	if errors.Is(err, ErrJobRunning) {
		return fail(http.StatusConflict, "job_running", "%v", err)
	}
	if err != nil {
		return fail(http.StatusInternalServerError, "internal", "%v", err)
	}
	return http.StatusAccepted, map[string]any{"job": job}
}
