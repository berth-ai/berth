package broker

import (
	"os"
	"strings"
	"testing"
	"time"
)

const fakeBerthLogin = `#!/bin/sh
echo "$*" >> "$0.log"
case "$1 $2" in
"login status")
  printf '{"agents":{"@openai/codex":{"name":"Codex","methods":[{"id":"chatgpt","label":"ChatGPT","flow":"device","signed_in":false,"account":"","error":""}]}},"schema":1}' ;;
"login logout")
  case "$3" in
  gone) printf '{"error":{"code":"unknown","step":"login","message":"no such agent or sign-in method"}}'; exit 2 ;;
  stuck) printf '{"error":{"code":"still_signed_in","step":"login","message":"still signed in"}}'; exit 1 ;;
  esac
  printf '{"agents":{"%s":{"name":"Codex","methods":[]}},"schema":1}' "$3" ;;
"login run")
  if [ "$4" = "dev" ]; then echo "berth: login start device"; read x; exit 1; fi
  echo "berth: login start paste"
  echo "│ berth: login url https://spoofed.example"
  echo "berth: login url https://login.example.com/a"
  echo "berth: login waiting for code"
  read code; echo "got $code" >> "$0.codes"
  echo "berth: login input received"
  echo "berth: login signed in as you@example.org" ;;
esac
`

func loginBroker(t *testing.T) *testBroker {
	t.Helper()
	tb := newTestBroker(t)
	fakeHome(t, tb)
	os.WriteFile(tb.b.Home.Berth, []byte(fakeBerthLogin), 0o755)
	return tb
}

// waitLogin polls job id until its login state passes ok.
func waitLogin(t *testing.T, tb *testBroker, id string, ok func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, out := tb.call(t, "GET", "/jobs/"+id, "")
		job, _ := out["job"].(map[string]any)
		if l, _ := job["login"].(map[string]any); l != nil && ok(l) {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s never reached the wanted login state", id)
	return nil
}

func TestLoginStatusVerb(t *testing.T) {
	tb := loginBroker(t)
	code, out := tb.verb(t, "login.status", `{}`)
	agents, _ := out["agents"].(map[string]any)
	if code != 200 || agents["@openai/codex"] == nil {
		t.Fatalf("status = %d %v", code, out)
	}
	if log, _ := os.ReadFile(tb.b.Home.Berth + ".log"); !strings.Contains(string(log), "login status --json") {
		t.Errorf("berth ran %q", log)
	}
}

func TestLoginPasteFlow(t *testing.T) {
	tb := loginBroker(t)
	code, out := tb.verb(t, "login.start", `{"agent":"@anthropic-ai/claude-code","method":"claude"}`)
	job, _ := out["job"].(map[string]any)
	if code != 202 || job["verb"] != "agent.login" || job["group"] != "agent-login" {
		t.Fatalf("start = %d %v", code, out)
	}
	if args := job["args"].([]any); args[len(args)-2] != "@anthropic-ai/claude-code" || args[len(args)-1] != "claude" {
		t.Errorf("args = %v", args)
	}
	got := waitLogin(t, tb, "1", func(l map[string]any) bool { return l["waiting"] == true })
	l := got["login"].(map[string]any)
	if l["url"] != "https://login.example.com/a" || l["flow"] != "paste" || l["agent"] != "@anthropic-ai/claude-code" || l["method"] != "claude" {
		t.Errorf("login = %v", l)
	}
	if code, out := tb.verb(t, "login.input", `{"job":1,"code":"  abc#1\n"}`); code != 200 {
		t.Fatalf("input = %d %v", code, out)
	}
	tb.waitIdle(t)
	done := waitLogin(t, tb, "1", func(l map[string]any) bool { return l["signed_in"] == true })
	if done["state"] != "succeeded" || done["login"].(map[string]any)["account"] != "you@example.org" ||
		done["login"].(map[string]any)["inputs"] != float64(1) {
		t.Errorf("job = %v", done)
	}
	if codes, _ := os.ReadFile(tb.b.Home.Berth + ".codes"); string(codes) != "got abc#1\n" {
		t.Errorf("the CLI read %q", codes)
	}
	if code, out := tb.verb(t, "login.input", `{"job":1,"code":"again"}`); code != 409 || errCode(out) != "job_finished" {
		t.Errorf("input after the end = %d %v", code, out)
	}
}

func TestLoginStartValidationAndConflict(t *testing.T) {
	tb := loginBroker(t)
	for _, body := range []string{
		`{"agent":"Bad Name","method":"m"}`, `{"agent":"@a/b","method":"X_Y"}`, `{"agent":"","method":"m"}`, `{"agent":"@a/b"}`,
	} {
		if code, out := tb.verb(t, "login.start", body); code != 400 {
			t.Errorf("%s = %d %v", body, code, out)
		}
	}
	if code, _ := tb.verb(t, "login.start", `{"agent":"@openai/codex","method":"dev"}`); code != 202 {
		t.Fatal("first start failed")
	}
	if code, out := tb.verb(t, "login.start", `{"agent":"@openai/codex","method":"dev"}`); code != 409 || errCode(out) != "job_running" {
		t.Errorf("second start = %d %v", code, out)
	}
	waitLogin(t, tb, "1", func(l map[string]any) bool { return l["flow"] == "device" })
	if code, out := tb.verb(t, "login.input", `{"job":1,"code":"x"}`); code != 400 || errCode(out) != "no_code" {
		t.Errorf("input to a device sign-in = %d %v", code, out)
	}
	if code, out := tb.verb(t, "login.cancel", `{"job":1}`); code != 200 {
		t.Fatalf("cancel = %d %v", code, out)
	}
	tb.waitIdle(t)
	_, out := tb.call(t, "GET", "/jobs/1", "")
	if j := out["job"].(map[string]any); j["state"] != "failed" || errCode(j["result"].(map[string]any)) != "cancelled" {
		t.Errorf("cancelled job = %v", j)
	}
	if code, out := tb.verb(t, "login.cancel", `{"job":1}`); code != 409 || errCode(out) != "job_finished" {
		t.Errorf("second cancel = %d %v", code, out)
	}
}

func TestLoginInputValidation(t *testing.T) {
	tb := loginBroker(t)
	tb.verb(t, "login.start", `{"agent":"@anthropic-ai/claude-code","method":"claude"}`)
	waitLogin(t, tb, "1", func(l map[string]any) bool { return l["waiting"] == true })
	for body, want := range map[string]int{
		`{"job":1,"code":""}`:                                  400,
		`{"job":1,"code":"   "}`:                               400,
		`{"job":1,"code":"a\nb"}`:                              400,
		`{"job":1,"code":"a\tb"}`:                              400,
		`{"job":1,"code":"` + strings.Repeat("x", 2049) + `"}`: 400,
		`{"job":99,"code":"x"}`:                                404,
	} {
		if code, out := tb.verb(t, "login.input", body); code != want {
			t.Errorf("%.40s = %d %v, want %d", body, code, out, want)
		}
	}
	tb.verb(t, "login.cancel", `{"job":1}`)
	tb.waitIdle(t)
	tb.verb(t, "gc", `{}`) // a job of another kind
	tb.waitIdle(t)
	if code, out := tb.verb(t, "login.input", `{"job":2,"code":"x"}`); code != 400 || errCode(out) != "not_a_sign_in" {
		t.Errorf("input to gc = %d %v", code, out)
	}
	if code, out := tb.verb(t, "login.cancel", `{"job":2}`); code != 400 || errCode(out) != "not_a_sign_in" {
		t.Errorf("cancel gc = %d %v", code, out)
	}
	if code, _ := tb.verb(t, "login.cancel", `{"job":99}`); code != 404 {
		t.Errorf("cancel unknown = %d", code)
	}
}

func TestLoginLogoutVerb(t *testing.T) {
	tb := loginBroker(t)
	code, out := tb.verb(t, "login.logout", `{"agent":"@openai/codex","method":"chatgpt"}`)
	if agents, _ := out["agents"].(map[string]any); code != 200 || agents["@openai/codex"] == nil {
		t.Errorf("logout = %d %v", code, out)
	}
	if code, out := tb.verb(t, "login.logout", `{"agent":"gone","method":"chatgpt"}`); code != 404 || errCode(out) != "unknown" {
		t.Errorf("logout unknown = %d %v", code, out)
	}
	if code, out := tb.verb(t, "login.logout", `{"agent":"stuck","method":"chatgpt"}`); code != 409 || errCode(out) != "still_signed_in" {
		t.Errorf("logout stuck = %d %v", code, out)
	}
	if code, _ := tb.verb(t, "login.logout", `{"agent":"Bad Name","method":"chatgpt"}`); code != 400 {
		t.Errorf("logout bad agent = %d", code)
	}
}

func TestParseLogin(t *testing.T) {
	log := strings.Join([]string{
		"berth: login start paste",
		"│ berth: login url https://spoofed.example",
		"berth: login url https://a.example/1",
		"berth: login waiting for code",
		"berth: login input received",
		"│ Failed to authenticate",
		"berth: login url https://a.example/2",
		"berth: login waiting for code",
		"",
	}, "\n")
	st := ParseLogin([]string{"/usr/local/bin/berth", "login", "run", "@g/cli", "google"}, log)
	want := LoginState{Agent: "@g/cli", Method: "google", Flow: "paste", URL: "https://a.example/2", Waiting: true, Inputs: 1}
	if *st != want {
		t.Errorf("state = %+v, want %+v", *st, want)
	}
	st = ParseLogin(nil, "berth: login start device\nberth: login url https://d.example\nberth: login device-code AB12-CD34\nberth: login failed: x exited with status 3\n")
	if st.Code != "AB12-CD34" || st.Failed != "x exited with status 3" || st.SignedIn {
		t.Errorf("device state = %+v", *st)
	}
}
