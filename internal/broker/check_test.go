package broker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/berth-ai/bert/internal/locksource"
)

func TestUpdateCheckCachesPerRef(t *testing.T) {
	tb := newTestBroker(t)
	tb.b.Load()
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	tb.b.Now = func() time.Time { return now }
	current := "aaaaaaaaaaaa"
	tb.b.CurrentHash = func() string { return current }
	var fetched []string
	tb.b.Fetch = func(_ context.Context, ref string) (*locksource.Lock, error) {
		fetched = append(fetched, ref)
		return &locksource.Lock{Hash: "bbbbbbbbbbbb", Source: locksource.Source{Commit: "c0ffee"}}, nil
	}
	_, out := tb.verb(t, "update.check", `{}`)
	c := out["check"].(map[string]any)
	if c["ref"] != "main" || c["hash"] != "bbbbbbbbbbbb" || c["update_available"] != true || c["commit"] != "c0ffee" {
		t.Fatalf("check = %v", c)
	}
	current = "bbbbbbbbbbbb"
	_, out = tb.verb(t, "update.check", `{}`)
	if out["check"].(map[string]any)["update_available"] != false || len(fetched) != 1 {
		t.Errorf("cached check = %v, fetched %v", out, fetched)
	}
	tb.verb(t, "update.check", `{"ref":"v2"}`)
	tb.verb(t, "update.check", `{"force":true}`)
	now = now.Add(61 * time.Minute)
	tb.verb(t, "update.check", `{"ref":"v2"}`)
	if len(fetched) != 4 || fetched[1] != "v2" || fetched[2] != "main" || fetched[3] != "v2" {
		t.Errorf("fetched = %v", fetched)
	}
	if st := tb.b.Status(); st.Check == nil || st.Check.Ref != "main" {
		t.Errorf("status check = %+v", st.Check)
	}
}

func TestUpdateCheckFailureIsUnknown(t *testing.T) {
	tb := newTestBroker(t)
	tb.b.Load()
	tb.b.Fetch = func(context.Context, string) (*locksource.Lock, error) {
		return nil, errors.New("dial tcp: no route to host")
	}
	code, out := tb.verb(t, "update.check", `{}`)
	c := out["check"].(map[string]any)
	if code != 200 || c["update_available"] != nil || c["error"] != "dial tcp: no route to host" {
		t.Errorf("check = %d %v", code, out)
	}
	if code, out := tb.verb(t, "update.check", `{"ref":"a b"}`); code != 400 {
		t.Errorf("bad ref = %d %v", code, out)
	}
}
