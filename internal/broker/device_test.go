package broker

import (
	"testing"
	"time"
)

func TestParseDevice(t *testing.T) {
	for name, c := range map[string]struct{ log, code, url string }{
		"gh without a clipboard": {
			"! First copy your one-time code: 1A2B-3C4D\nOpen this URL to continue in your web browser: https://github.com/login/device\n",
			"1A2B-3C4D", "https://github.com/login/device",
		},
		"gh with a clipboard": {
			"\n! One-time code (E2EB-A62A) copied to clipboard\nOpen this URL to continue in your web browser: https://github.com/login/device\n",
			"E2EB-A62A", "https://github.com/login/device",
		},
		"glab": {
			"WARNING: The operating system keyring is unavailable. Storing credentials as plaintext in the configuration file.\n" +
				"- Signing into gitlab.com\n\nFirst copy your one-time code: QPMCJSWK\n" +
				"Then open this URL on any device to authorize: https://gitlab.com/oauth/device\n\nWaiting for authorization...\n",
			"QPMCJSWK", "https://gitlab.com/oauth/device",
		},
		"glab with complete url": {
			"Open https://gitlab.com/oauth/device?user_code=WXYZ-9876 to authorize. Code: WXYZ-9876\n",
			"WXYZ-9876", "https://gitlab.com/oauth/device?user_code=WXYZ-9876",
		},
	} {
		d := ParseDevice(c.log)
		if d == nil || d.Code != c.code || d.URL != c.url {
			t.Errorf("%s: %+v", name, d)
		}
	}
	if ParseDevice("Logged in as someone\nWARNING: see https://example.com/device-docs\n") != nil {
		t.Error("no device in an ordinary log")
	}
	if d := ParseDevice("code: 1A2B-3C4D\n"); d != nil {
		t.Errorf("a code without a URL is not enough: %+v", d)
	}
}

func TestJobAnnotation(t *testing.T) {
	j := newTestJobs(t)
	j.Annotate = func(job *Job, log string) { job.Device = ParseDevice(log) }
	job, _ := j.Start("forge.login", "forge", nil, blocking(make(chan struct{}),
		"one-time code: 1A2B-3C4D https://github.com/login/device\n", 0, `{}`))
	waitJob(t, j, job.ID, StateRunning)
	// The work writes its log after the job starts running.
	var got Job
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if got, _ = j.Get(job.ID); got.Device != nil {
			break
		}
	}
	if got.Device == nil || got.Device.Code != "1A2B-3C4D" {
		t.Errorf("device = %+v", got.Device)
	}
	if list := j.List(); list[0].Device != nil {
		t.Error("lists carry no logs, so no device either")
	}
}
