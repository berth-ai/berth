package broker

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/pairqr"
	"github.com/berth-ai/berth/internal/supervisor"
)

// Orca's pairing files, in OrcaRunDir. The module reads and deletes the
// request when it starts and then sends Orca's readiness output to the
// readiness file, never to a log, because that output holds the link.
const (
	orcaRequestFile   = "pair-request.json"
	orcaReadinessFile = "readiness.json"
)

// orcaRequest asks the next start of the orca module to create a pairing
// offer for scope, telling clients to dial address.
type orcaRequest struct {
	Scope   string `json:"scope"`
	Address string `json:"address"`
}

// checkOrca refuses switching Orca on in an image that does not ship it.
// A config that already has it on stays, so the switch can be turned off.
func (b *Broker) checkOrca(cur, next config.Config) error {
	if !next.Frontends.Orca.Enabled || cur.Frontends.Orca.Enabled || b.OrcaInstalled == nil || b.OrcaInstalled() {
		return nil
	}
	return &config.ValidationError{Fields: []config.FieldError{{Field: "frontends.orca.enabled", Message: "Orca is not installed in this image"}}}
}

// orcaPair restarts Orca once with a pairing request and returns the link
// it printed. The link is a secret: it never reaches a log.
func (b *Broker) orcaPair(ctx context.Context, body []byte) (int, any) {
	var in struct {
		Scope string `json:"scope"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if in.Scope != "desktop" && in.Scope != "mobile" {
		return badRequest(errors.New("scope must be desktop or mobile"))
	}
	// Holding change serializes pairings with each other and with config
	// changes, so Orca cannot be switched off halfway through.
	b.change.Lock()
	defer b.change.Unlock()
	b.mu.Lock()
	cfg, running := b.cfg, slices.ContainsFunc(b.desired(), func(s supervisor.Spec) bool { return s.Name == "orca" })
	b.mu.Unlock()
	if !running {
		return fail(http.StatusConflict, "orca_disabled", "Orca is off; turn on frontends.orca.enabled first")
	}
	addr, warning, err := b.Host.OrcaAddress(cfg.Frontends.Orca.PairingAddress)
	if errors.Is(err, ErrOrcaAddressNeeded) {
		return fail(http.StatusConflict, "orca_address_needed", "%v", err)
	}
	dir := cmp.Or(b.OrcaRunDir, "/run/berth/orca")
	if err := b.orcaRunDir(dir); err != nil {
		return fail(http.StatusInternalServerError, "write_failed", "%v", err)
	}
	reqPath, readyPath := filepath.Join(dir, orcaRequestFile), filepath.Join(dir, orcaReadinessFile)
	// Removing both files on every return means a restart after a timeout
	// cannot mint a link nobody sees.
	defer os.Remove(reqPath)
	defer os.Remove(readyPath)
	if err := os.Remove(readyPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fail(http.StatusInternalServerError, "write_failed", "%v", err)
	}
	raw, _ := json.Marshal(orcaRequest{Scope: in.Scope, Address: addr})
	uid, gid := b.agentUser()
	if err := config.WriteFileAtomic(reqPath, raw, 0o600, uid, gid); err != nil {
		return fail(http.StatusInternalServerError, "write_failed", "%v", err)
	}
	if err := b.Services.Restart("orca"); err != nil {
		return fail(http.StatusInternalServerError, "restart_failed", "%v", err)
	}
	b.logf("orca: restarted to pair a %s", in.Scope)
	timeout := cmp.Or(b.OrcaPairTimeout, time.Minute)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		if p, ok := readOrcaPairing(readyPath); ok {
			if !p.Available || p.URL == "" {
				return fail(http.StatusBadGateway, "orca_pairing_unavailable", "Orca could not create a pairing link: %s",
					strings.Join(slices.DeleteFunc([]string{p.Reason, p.Guidance}, func(s string) bool { return s == "" }), ". "))
			}
			rows, err := pairqr.Matrix(p.URL)
			if err != nil {
				return fail(http.StatusInternalServerError, "qr_failed", "%v", err)
			}
			out := map[string]any{"url": p.URL, "scope": in.Scope, "address": addr, "qr": rows}
			if warning != "" {
				out["warning"] = warning
			}
			return http.StatusOK, out
		}
		select {
		case <-ctx.Done():
			return fail(http.StatusGatewayTimeout, "orca_pair_timeout",
				"Orca did not print a pairing link within %s; see the orca service log", timeout)
		case <-tick.C:
		}
	}
}

// orcaRunDir creates dir for agent alone: the module runs as agent and
// nobody else may read a link.
func (b *Broker) orcaRunDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	if uid, gid := b.agentUser(); uid != -1 {
		return os.Chown(dir, uid, gid)
	}
	return nil
}

// agentUser is agent:agent, the owner of files agent alone reads; -1, -1
// keeps the owner when there is no agent user.
func (b *Broker) agentUser() (uid, gid int) {
	if b.Agent == nil {
		return -1, -1
	}
	return int(b.Agent.UID), int(b.Agent.GID)
}

type orcaPairing struct {
	Available bool   `json:"available"`
	URL       string `json:"url"`
	Reason    string `json:"reason"`
	Guidance  string `json:"guidance"`
}

// readOrcaPairing finds the first JSON object with a pairing key in Orca's
// readiness output, which may be one object, several lines, or still being
// written.
func readOrcaPairing(path string) (orcaPairing, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return orcaPairing{}, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	for {
		var v struct {
			Pairing *orcaPairing `json:"pairing"`
		}
		if err := dec.Decode(&v); err != nil {
			if !errors.Is(err, io.EOF) {
				// A line that is not JSON: skip to the next one.
				rest, _ := io.ReadAll(dec.Buffered())
				i := bytes.IndexByte(rest, '\n')
				if i < 0 {
					return orcaPairing{}, false
				}
				dec = json.NewDecoder(bytes.NewReader(rest[i+1:]))
				continue
			}
			return orcaPairing{}, false
		}
		if v.Pairing != nil {
			return *v.Pairing, true
		}
	}
}
