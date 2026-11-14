package broker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/berth-ai/berth/internal/status"
)

func TestUpdateCheckCachesAndFollowsWhatRuns(t *testing.T) {
	tb := newTestBroker(t)
	tb.b.Load()
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	tb.b.Now = func() time.Time { return now }
	running := map[string]string{"@openai/codex": "0.167.0", "gh": "2.103.3"}
	tb.b.Agents = func() (status.Report, error) {
		return status.Report{Pins: []status.Pin{
			{Name: "@openai/codex", Track: "agents", Image: "0.167.0", Current: running["@openai/codex"]},
			{Name: "gh", Track: "tools", Image: "2.103.3", Current: running["gh"]},
		}}, nil
	}
	calls := 0
	tb.b.CheckPins = func(context.Context) ([]status.Pin, error) {
		calls++
		return []status.Pin{
			{Name: "@openai/codex", Track: "agents", Image: "0.167.0", Current: "0.167.0", Latest: "0.170.0"},
			{Name: "gh", Track: "tools", Image: "2.103.3", Current: "2.103.3", Latest: "3.0.0"},
			{Name: "uv", Track: "tools", Image: "0.13.7", Current: "0.13.7", Error: "no versions"},
		}, nil
	}
	_, out := tb.verb(t, "update.check", `{}`)
	c := out["check"].(map[string]any)
	if c["updates"] != 1.0 || c["majors"] != 1.0 || c["update_available"] != true || len(c["pins"].([]any)) != 3 {
		t.Fatalf("check = %v", c)
	}
	running["@openai/codex"] = "0.170.0" // updated since the check
	_, out = tb.verb(t, "update.check", `{}`)
	if c := out["check"].(map[string]any); c["updates"] != 0.0 || c["majors"] != 1.0 || calls != 1 {
		t.Errorf("cached check = %v after %d lookups", c, calls)
	}
	tb.verb(t, "update.check", `{"force":true}`)
	now = now.Add(61 * time.Minute)
	tb.verb(t, "update.check", `{}`)
	if calls != 3 {
		t.Errorf("lookups = %d, want 3", calls)
	}
	if st := tb.b.Status(); st.Check == nil || st.Check.Majors != 1 {
		t.Errorf("status check = %+v", st.Check)
	}
}

func TestUpdateCheckFailureIsUnknown(t *testing.T) {
	tb := newTestBroker(t)
	tb.b.Load()
	tb.b.CheckPins = func(context.Context) ([]status.Pin, error) {
		return nil, errors.New("dial tcp: no route to host")
	}
	code, out := tb.verb(t, "update.check", `{}`)
	c := out["check"].(map[string]any)
	if code != 200 || c["update_available"] != nil || c["error"] != "dial tcp: no route to host" {
		t.Errorf("check = %d %v", code, out)
	}
	if code, out := tb.verb(t, "update.check", `{"ref":"main"}`); code != 400 {
		t.Errorf("a ref = %d %v", code, out)
	}
	if st := tb.b.Status(); st.Check != nil {
		t.Errorf("a failed check was cached: %+v", st.Check)
	}
}

func TestUpdateVersionsRejects(t *testing.T) {
	tb := newTestBroker(t)
	for _, body := range []string{`{"name":"gh","track":"other"}`, `{"name":"-x","track":"tools"}`, `{"name":"a b","track":"agents"}`} {
		if code, out := tb.verb(t, "update.versions", body); code != 400 {
			t.Errorf("%s = %d %v", body, code, out)
		}
	}
}
