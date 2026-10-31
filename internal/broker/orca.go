package broker

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
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

// orcaDevicesFile is Orca's device registry in its home. orca-ide serve
// keeps its profile where the packaged app does, ~/.config/orca on Linux,
// whether it runs Electron or orcad; ~/.orca is only standalone orcad's.
// XDG_CONFIG_HOME or ORCA_USER_DATA_PATH would move it; the image sets
// neither.
const orcaDevicesFile = ".config/orca/orca-devices.json"

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
// readiness output. That is one line, which may follow other output or
// carry a prefix; pretty-printed JSON is read too. A file still being
// written yields nothing until the object is complete.
func readOrcaPairing(path string) (orcaPairing, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return orcaPairing{}, false
	}
	type ready struct {
		Pairing *orcaPairing `json:"pairing"`
	}
	for line := range bytes.Lines(raw) {
		var v ready
		if i := bytes.IndexByte(line, '{'); i >= 0 && json.Unmarshal(line[i:], &v) == nil && v.Pairing != nil {
			return *v.Pairing, true
		}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	for {
		var v ready
		if err := dec.Decode(&v); err != nil {
			return orcaPairing{}, false
		}
		if v.Pairing != nil {
			return *v.Pairing, true
		}
	}
}

// orcaDevice is one row of orca.devices. Times are nil when Orca has none.
type orcaDevice struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Scope      string     `json:"scope"`
	PairedAt   *time.Time `json:"paired_at"`
	LastSeenAt *time.Time `json:"last_seen_at"`
}

// orcaDevices lists the devices paired with Orca, read from its registry.
// The registry holds each device's token; only the fields above leave it.
func (b *Broker) orcaDevices(_ context.Context, body []byte) (int, any) {
	if err := decode(body, &struct{}{}); err != nil {
		return badRequest(err)
	}
	home := b.OrcaHome
	if home == "" && b.Agent != nil {
		home = b.Agent.Home
	}
	if home == "" {
		return fail(http.StatusInternalServerError, "no_agent_user", "there is no agent home to read Orca's devices from")
	}
	devices, err := readOrcaDevices(home)
	if err != nil {
		return fail(http.StatusBadGateway, "orca_devices_unreadable", "could not read Orca's paired devices: %v", err)
	}
	return http.StatusOK, map[string]any{"devices": devices}
}

// orcaDevicesMax bounds the registry read; a real one is a few KiB.
const orcaDevicesMax = 4 << 20

// readOrcaDevices reads the registry in home as Orca 1.4 writes it: a JSON
// array of rows. It opens the file through home as an os.Root, because
// agent owns both and root must not follow a link out of the home. A
// missing file means no devices. Rows Orca would not count as a paired
// device are left out: a link nobody used yet (a token, never seen) and a
// browser's session-only key.
func readOrcaDevices(home string) ([]orcaDevice, error) {
	root, err := os.OpenRoot(home)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(orcaDevicesFile)
	if errors.Is(err, fs.ErrNotExist) {
		return []orcaDevice{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, orcaDevicesMax+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > orcaDevicesMax {
		return nil, fmt.Errorf("%s is over %d bytes", orcaDevicesFile, orcaDevicesMax)
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("%s is not a JSON array: %w", orcaDevicesFile, err)
	}
	devices := []orcaDevice{}
	for _, r := range rows {
		var row struct {
			DeviceID       string `json:"deviceId"`
			Name           string `json:"name"`
			Token          string `json:"token"`
			Scope          string `json:"scope"`
			PairedAt       int64  `json:"pairedAt"`
			LastSeenAt     int64  `json:"lastSeenAt"`
			CredentialKind string `json:"credentialKind"`
			Ephemeral      bool   `json:"ephemeral"`
		}
		if json.Unmarshal(r, &row) != nil || row.DeviceID == "" || row.Ephemeral {
			continue
		}
		if row.LastSeenAt == 0 && row.Token != "" && row.CredentialKind == "" {
			continue
		}
		// Orca calls a paired desktop a runtime; orca.pair calls it a desktop.
		scope := row.Scope
		if scope == "runtime" {
			scope = "desktop"
		}
		devices = append(devices, orcaDevice{ID: row.DeviceID, Name: row.Name, Scope: scope,
			PairedAt: msTime(row.PairedAt), LastSeenAt: msTime(row.LastSeenAt)})
	}
	return devices, nil
}

// msTime is a JavaScript timestamp in UTC; 0 is nil.
func msTime(ms int64) *time.Time {
	if ms <= 0 {
		return nil
	}
	t := time.UnixMilli(ms).UTC()
	return &t
}
