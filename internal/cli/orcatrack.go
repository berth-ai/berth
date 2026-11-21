package cli

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"

	"github.com/berth-ai/berth/internal/agentstore"
	"github.com/berth-ai/berth/internal/broker"
	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/orcastore"
	"github.com/berth-ai/berth/internal/output"
	"github.com/berth-ai/berth/internal/pins"
	"github.com/berth-ai/berth/internal/status"
)

// orcaPin is the image's Orca, or nil when the image has none for this
// architecture: then the Orca track is off.
func (a *app) orcaPin() *orcastore.Pin {
	if a.cfg.OrcaDir == "" || a.cfg.OrcaPin == "" {
		return nil
	}
	p, err := orcastore.ReadPin(a.cfg.OrcaPin)
	if err != nil {
		a.p.Logf("warning: orca: %v", err)
		return nil
	}
	if !p.Supports(a.arch()) {
		return nil
	}
	return p
}

// orcaStore is the Orca store; a zero Store when the Orca track is off.
func (a *app) orcaStore() agentstore.Store {
	if a.orcaPin() == nil {
		return agentstore.Store{}
	}
	return agentstore.Store{Root: a.cfg.OrcaDir}
}

func (a *app) orcaOn() bool { return a.orcaStore().Root != "" }

// orcaImage is the image's Orca release, empty when the track is off.
func (a *app) orcaImage() string {
	if p := a.orcaPin(); p != nil {
		return p.Version
	}
	return ""
}

func (a *app) orcaInstaller(pin *orcastore.Pin) orcastore.Installer {
	return orcastore.Installer{
		Store: agentstore.Store{Root: a.cfg.OrcaDir}, Pin: pin, Arch: a.arch(), Releases: a.cfg.OrcaReleases,
		Runner: a.cfg.Runner, DpkgDeb: a.cfg.DpkgDeb, Root: a.cfg.Agent != nil, Now: a.cfg.Now, Logf: a.p.Logf,
	}
}

// orcaEnabled reports whether the config switches Orca on. An unreadable
// config counts as off: boot then installs nothing it may not need.
func (a *app) orcaEnabled() bool {
	if a.cfg.ConfigDir == "" {
		return false
	}
	c, err := (&config.Store{Dir: a.cfg.ConfigDir}).Load()
	return err == nil && c.Frontends.Orca.Enabled
}

// orcaNow reads the image's Orca release, its pin and the Orca overrides.
func (a *app) orcaNow() (string, status.Pin, pins.Overrides, error) {
	image, s := a.orcaImage(), a.orcaStore()
	cur, err := s.Current()
	if err != nil {
		return "", status.Pin{}, nil, err
	}
	st, err := s.ReadState()
	if err != nil {
		return "", status.Pin{}, nil, err
	}
	return image, status.OrcaPin(image, cur, st.Overrides), cloneOverrides(st.Overrides), nil
}

// convergeOrca makes Orca the release ov picks, or the image's. It installs
// only while Orca is on, when a version is already installed, or with
// force; otherwise it records ov for the moment Orca is switched on. With
// restart, a running Orca restarts onto a new version.
func (a *app) convergeOrca(ctx context.Context, ov pins.Overrides, force, restart bool) (*trackResult, error) {
	s := a.orcaStore()
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return nil, output.Errorf(output.ExitFailure, "internal", "prepare", "%v", err)
	}
	pin := a.orcaPin()
	want := pin.Version
	if o, ok := ov[orcastore.Name]; ok {
		want = o.Version
	}
	cur, err := s.Current()
	if err != nil {
		return nil, output.Errorf(output.ExitFailure, "internal", "orca", "%v", err)
	}
	res := &trackResult{}
	if force || cur != nil || a.orcaEnabled() {
		v, installed, err := a.orcaInstaller(pin).Install(ctx, want)
		if err != nil {
			return nil, installErr(err)
		}
		res.Installed = installed
		if res.Changed, err = s.Flip(v.Hash); err != nil {
			return nil, output.Errorf(output.ExitFailure, "flip_failed", "flip", "%v", err)
		}
	}
	if err := s.WriteState(agentstore.State{FollowImage: len(ov) == 0, Overrides: ov}); err != nil {
		return nil, output.Errorf(output.ExitFailure, "flip_failed", "flip", "%v", err)
	}
	if res.Current, res.Previous, err = storeViews(s); err != nil {
		return nil, output.Errorf(output.ExitFailure, "internal", "flip", "%v", err)
	}
	if res.Changed && restart {
		a.restartOrca(ctx)
	}
	return res, nil
}

// ensureOrca drops an Orca override a newer image caught up with and, while
// Orca is on or installed, installs the release it picks. Failures are
// logged: Orca never blocks boot, and its service waits for a version.
func (a *app) ensureOrca(ctx context.Context) *ensureTrack {
	res := &ensureTrack{Action: "none", Reason: "up-to-date", Dropped: []string{}}
	image, pin, ov, err := a.orcaNow()
	if err != nil {
		a.p.Logf("warning: orca: %v", err)
		res.Reason = "unreadable"
		return res
	}
	ov, dropped := pins.Rebase(ov, map[string]string{orcastore.Name: image})
	res.Dropped = orEmpty(dropped)
	cur, err := a.orcaStore().Current()
	if err != nil {
		a.p.Logf("warning: orca: %v", err)
		res.Reason = "unreadable"
		return res
	}
	want := pins.Desired(map[string]string{orcastore.Name: image}, ov)[orcastore.Name]
	switch {
	case cur == nil && !a.orcaEnabled():
		res.Reason = "off"
	case cur != nil && pin.Current == want:
	default:
		t, err := a.convergeOrca(ctx, ov, false, false)
		if err != nil {
			a.p.Logf("warning: orca: %v; Orca starts once berth orca install succeeds", err)
			res.Reason = "unresolved"
			return res
		}
		res.Action, res.Reason = "installed", "release-changed"
		if t.Current != nil && !t.Installed {
			res.Action = "switched"
		}
		return res
	}
	if err := a.orcaStore().WriteState(agentstore.State{FollowImage: len(ov) == 0, Overrides: ov}); err != nil {
		a.p.Logf("warning: orca: %v", err)
	}
	return res
}

// runOrcaInstall is berth orca install: it installs the release Orca
// should run, whether or not Orca is on. The broker runs it when Orca is
// switched on with nothing installed; Orca's service waits for it.
func runOrcaInstall(ctx context.Context, a *app) error {
	if !a.orcaOn() {
		return output.Errorf(output.ExitFailure, "orca_unsupported", "", "this image has no Orca for its architecture")
	}
	return a.locked("orca install", func() error {
		image, _, ov, err := a.orcaNow()
		if err != nil {
			return output.Errorf(output.ExitFailure, "internal", "orca", "%v", err)
		}
		ov, _ = pins.Rebase(ov, map[string]string{orcastore.Name: image})
		res, err := a.convergeOrca(ctx, ov, true, true)
		if err != nil {
			return err
		}
		a.gcQuietly()
		return a.p.Result(res, func(w io.Writer) {
			fmt.Fprintf(w, "Orca %s (%s)\n", orcaVersion(res.Current), hashOrNone(res.Current))
		})
	})
}

// restartOrca asks the supervisor to restart Orca onto the current version.
// Orca off, or no supervisor, needs no restart.
func (a *app) restartOrca(ctx context.Context) {
	if a.cfg.BrokerSocket == "" {
		return
	}
	if _, err := os.Stat(a.cfg.BrokerSocket); err != nil {
		return
	}
	code, raw, err := broker.NewClient(a.cfg.BrokerSocket).Verb(ctx, "service.restart", map[string]string{"name": "orca"})
	switch {
	case err != nil:
		a.p.Logf("warning: could not restart Orca onto the new version: %v", err)
	case code == http.StatusNotFound: // Orca is off
	case code != http.StatusOK:
		a.p.Logf("warning: could not restart Orca onto the new version: %s", raw)
	}
}

// arch is the architecture Orca installs for.
func (a *app) arch() string { return cmp.Or(a.cfg.Arch, runtime.GOARCH) }

func storeViews(s agentstore.Store) (*status.VersionView, *status.VersionView, error) {
	cur, err := s.Current()
	if err != nil {
		return nil, nil, err
	}
	prev, err := s.Previous()
	if err != nil {
		return nil, nil, err
	}
	return status.View(cur), status.View(prev), nil
}

func orcaVersion(v *status.VersionView) string {
	if v == nil {
		return "not installed"
	}
	return v.Packages[orcastore.Name]
}
