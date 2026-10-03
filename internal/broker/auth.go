package broker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/berth-ai/bert/internal/config"
	"github.com/berth-ai/bert/internal/login"
)

// ModeSetup means no login exists yet: only the panel runs, and only
// setup.complete with the logged token can leave it.
const ModeSetup = "setup"

// verifyInterval spaces out password checks across all callers, so that
// even a client that dodges the panel's per-client backoff cannot guess
// quickly.
const verifyInterval = 250 * time.Millisecond

// enterSetup issues a new token and logs the setup link. Caller holds b.mu.
func (b *Broker) enterSetup() string {
	b.mode, b.token = ModeSetup, login.NewToken()
	url := b.Host.SetupURL(b.token)
	b.logf("setup: open %s", url)
	return url
}

func (b *Broker) authVerify(_ context.Context, body []byte) (int, any) {
	var in struct {
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if _, mode, _ := b.snapshot(); mode == ModeSetup {
		return fail(http.StatusConflict, "setup_required", "no login exists yet; finish setup first")
	}
	b.verifyMu.Lock()
	defer b.verifyMu.Unlock()
	if wait := verifyInterval - time.Since(b.lastVerify); wait > 0 {
		time.Sleep(wait)
	}
	defer func() { b.lastVerify = time.Now() }()
	ok, err := b.Logins.Verify(in.User, in.Password)
	if err != nil {
		return fail(http.StatusInternalServerError, "internal", "%v", err)
	}
	return http.StatusOK, map[string]any{"valid": ok}
}

func (b *Broker) authChange(_ context.Context, body []byte) (int, any) {
	var in struct {
		CurrentPassword string `json:"current_password"`
		User            string `json:"user"`
		Password        string `json:"password"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	b.change.Lock()
	defer b.change.Unlock()
	if _, mode, _ := b.snapshot(); mode == ModeSetup {
		return fail(http.StatusConflict, "setup_required", "no login exists yet; finish setup first")
	}
	user, err := b.Logins.User()
	if err != nil {
		return fail(http.StatusInternalServerError, "internal", "%v", err)
	}
	ok, err := b.Logins.Verify(user, in.CurrentPassword)
	if err != nil {
		return fail(http.StatusInternalServerError, "internal", "%v", err)
	}
	if !ok {
		return fail(http.StatusForbidden, "wrong_password", "the current password is wrong")
	}
	if code, v := checkLogin(in.User, in.Password); code != 0 {
		return code, v
	}
	if err := b.Logins.Set(in.User, in.Password); err != nil {
		return fail(http.StatusInternalServerError, "write_failed", "%v", err)
	}
	b.logf("panel login changed")
	return http.StatusOK, map[string]any{"user": in.User}
}

func (b *Broker) setupComplete(_ context.Context, body []byte) (int, any) {
	var in struct {
		Token    string          `json:"token"`
		User     string          `json:"user"`
		Password string          `json:"password"`
		Patch    json.RawMessage `json:"patch"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	b.change.Lock()
	defer b.change.Unlock()
	b.mu.Lock()
	mode, token := b.mode, b.token
	b.mu.Unlock()
	if mode != ModeSetup {
		return fail(http.StatusConflict, "not_in_setup", "setup is already complete")
	}
	if !login.TokenEqual(token, in.Token) {
		return fail(http.StatusForbidden, "invalid_token", "the setup link is not valid; use the newest one from the container log")
	}
	if code, v := checkLogin(in.User, in.Password); code != 0 {
		return code, v
	}

	// Check the wizard's settings before writing anything.
	cur, err := b.Store.Load()
	var ce *config.CorruptError
	corrupt := errors.As(err, &ce)
	switch {
	case corrupt:
		cur = config.Defaults()
	case err != nil:
		return fail(http.StatusInternalServerError, "internal", "%v", err)
	}
	if len(in.Patch) > 0 {
		next, err := config.Patch(cur, in.Patch)
		if err == nil {
			err = next.Validate()
		}
		if err != nil {
			return b.changed(cur, cur, err)
		}
	}

	if corrupt {
		if _, cur, err = b.Store.Reset(); err != nil {
			return fail(http.StatusInternalServerError, "write_failed", "%v", err)
		}
	}
	if err := b.Logins.Set(in.User, in.Password); err != nil {
		return fail(http.StatusInternalServerError, "write_failed", "%v", err)
	}
	b.mu.Lock()
	b.token = ""
	b.mu.Unlock()
	b.logf("setup complete: panel login %q created", in.User)
	next := cur
	if len(in.Patch) > 0 {
		var old config.Config
		old, next, err = b.Store.Apply(in.Patch, cur.Revision)
		if code, v := b.changed(old, next, err); code != http.StatusOK {
			return code, v
		}
	} else {
		b.changed(cur, cur, nil)
	}
	return http.StatusOK, map[string]any{"user": in.User, "config": next}
}

func (b *Broker) authReset(_ context.Context, body []byte) (int, any) {
	if err := decode(body, &struct{}{}); err != nil {
		return badRequest(err)
	}
	b.change.Lock()
	defer b.change.Unlock()
	if err := b.Logins.Delete(); err != nil {
		return fail(http.StatusInternalServerError, "write_failed", "%v", err)
	}
	b.mu.Lock()
	url := b.enterSetup()
	specs := b.desired()
	b.mu.Unlock()
	b.Services.Reconcile(specs)
	return http.StatusOK, map[string]any{"setup_url": url}
}

// checkLogin validates a new user and password; code 0 means valid.
func checkLogin(user, password string) (int, any) {
	var fields []config.FieldError
	if err := login.ValidUser(user); err != nil {
		fields = append(fields, config.FieldError{Field: "user", Message: err.Error()})
	}
	if err := login.ValidPassword(password); err != nil {
		fields = append(fields, config.FieldError{Field: "password", Message: err.Error()})
	}
	if fields == nil {
		return 0, nil
	}
	return http.StatusBadRequest, map[string]any{"error": map[string]any{
		"code": "invalid_login", "message": "the login is not valid", "fields": fields}}
}
