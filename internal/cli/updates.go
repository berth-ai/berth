package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/berth-ai/berth/internal/agentstore"
	"github.com/berth-ai/berth/internal/locksource"
	"github.com/berth-ai/berth/internal/orcastore"
	"github.com/berth-ai/berth/internal/output"
	"github.com/berth-ai/berth/internal/pins"
	"github.com/berth-ai/berth/internal/status"
	"github.com/berth-ai/berth/internal/toolstore"
	"github.com/berth-ai/berth/internal/upstream"
)

// checkWorkers bounds the version lookups a check runs at once.
const checkWorkers = 6

// latestKeyword asks for the newest version, as NAME@latest.
const latestKeyword = "latest"

func runStatus(ctx context.Context, a *app, args []string) error {
	fs := newFlagSet("status")
	check := fs.Bool("check", false, "look up the newest version of every pinned entity")
	jsonOut := fs.Bool("json", false, "print JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return usageErr(err)
	}
	if len(pos) > 0 {
		return usageErr(fmt.Errorf("status takes no arguments"))
	}
	a.p.JSON = *jsonOut
	rep, err := status.Collect(a.statusInputs(a.cfg.MiseConfig))
	if err != nil {
		return output.Errorf(output.ExitFailure, "internal", "status", "%v", err)
	}
	if *check {
		rep.Pins = a.check(ctx, rep.Pins)
		at := a.cfg.Now().UTC()
		rep.CheckedAt = &at
	}
	return a.p.Result(rep, func(w io.Writer) { status.WriteHuman(w, rep) })
}

// statusInputs are the paths status reads; miseConfig is the user track.
func (a *app) statusInputs(miseConfig string) status.Inputs {
	return status.Inputs{
		Store: a.store, ToolStore: a.toolStore(), SeedDir: a.cfg.SeedDir, BaseTools: a.cfg.BaseTools,
		OrcaStore: a.orcaStore(), OrcaImage: a.orcaImage(), BuildInfo: a.cfg.BuildInfo, MiseConfig: miseConfig,
	}
}

// toolStore is the base-tools store; a zero Store when tools are off.
func (a *app) toolStore() agentstore.Store {
	if a.cfg.ToolsDir == "" || a.cfg.BaseTools == "" {
		return agentstore.Store{}
	}
	return agentstore.Store{Root: a.cfg.ToolsDir}
}

func (a *app) toolsOn() bool { return a.toolStore().Root != "" }

func (a *app) mise() upstream.Mise {
	return upstream.Mise{Path: a.cfg.Mise, Runner: a.cfg.Runner, Env: []string{"MISE_YES=1"}}
}

// check fills Latest, Update and Error of every pin from upstream.
func (a *app) check(ctx context.Context, in []status.Pin) []status.Pin {
	out := slices.Clone(in)
	sem := make(chan struct{}, checkWorkers)
	var wg sync.WaitGroup
	for i := range out {
		wg.Add(1)
		sem <- struct{}{}
		go func(p *status.Pin) {
			defer func() { <-sem; wg.Done() }()
			latest, err := a.latest(ctx, *p)
			if err != nil {
				p.Error = strings.TrimPrefix(err.Error(), upstream.ErrLookup.Error()+": ")
				return
			}
			p.Latest, p.Update = latest, pins.Bump(p.Current, latest)
		}(&out[i])
	}
	wg.Wait()
	return out
}

func (a *app) latest(ctx context.Context, p status.Pin) (string, error) {
	switch p.Track {
	case status.TrackTools:
		return a.mise().Latest(ctx, p.Name)
	case status.TrackOrca:
		return a.orcaInstaller(nil).Latest(ctx)
	}
	return a.cfg.NPM.Latest(ctx, p.Name)
}

// pick is one entity an update moves.
type pick struct {
	Name  string `json:"name"`
	Track string `json:"track"`
	From  string `json:"from"`
	To    string `json:"to"`
	Bump  string `json:"bump,omitempty"`
}

// trackResult is what an update did to one store.
type trackResult struct {
	Changed   bool                `json:"changed"`
	Installed bool                `json:"installed"`
	Current   *status.VersionView `json:"current"`
	Previous  *status.VersionView `json:"previous"`
}

// updateResult keeps the agent set's fields at the top level, as before
// base tools existed.
type updateResult struct {
	Changed     bool                `json:"changed"`
	Installed   bool                `json:"installed"`
	FollowImage bool                `json:"follow_image"`
	Current     *status.VersionView `json:"current"`
	Previous    *status.VersionView `json:"previous"`
	Tools       *trackResult        `json:"tools,omitempty"`
	Orca        *trackResult        `json:"orca,omitempty"`
	Picked      []pick              `json:"picked"`
	Held        []pick              `json:"held"`
}

func (r updateResult) human(w io.Writer) {
	for _, p := range r.Picked {
		fmt.Fprintf(w, "%s %s -> %s\n", p.Name, p.From, p.To)
	}
	for _, p := range r.Held {
		fmt.Fprintf(w, "held %s %s -> %s (a major; run berth update %s)\n", p.Name, p.From, p.To, p.Name)
	}
	switch {
	case !r.Changed && (r.Tools == nil || !r.Tools.Changed) && (r.Orca == nil || !r.Orca.Changed):
		fmt.Fprintln(w, "nothing to update")
	default:
		fmt.Fprintf(w, "agents at %s, base tools %s\n", hashOrNone(r.Current), toolsState(r.Tools))
		if r.Orca != nil && r.Orca.Changed {
			fmt.Fprintf(w, "Orca at %s\n", orcaVersion(r.Orca.Current))
		}
	}
}

func toolsState(t *trackResult) string {
	if t == nil || t.Current == nil {
		return "as in the image"
	}
	return "at " + t.Current.Hash
}

func runUpdate(ctx context.Context, a *app, args []string) error {
	fs := newFlagSet("update")
	major := fs.Bool("major", false, "include major updates when no NAME is given")
	image := fs.Bool("image", false, "return to the image's version (of every entity, or of each NAME)")
	lockDir := fs.String("lock-dir", "", "install the agent set from a local directory")
	jsonOut := fs.Bool("json", false, "print JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return usageErr(err)
	}
	a.p.JSON = *jsonOut
	switch {
	case *lockDir != "" && (*image || len(pos) > 0 || *major):
		return usageErr(errors.New("--lock-dir takes no NAME, --image or --major"))
	case *image && *major:
		return usageErr(errors.New("--image and --major are mutually exclusive"))
	}
	targets := make([]target, 0, len(pos))
	for _, arg := range pos {
		t, err := parseTarget(arg)
		if err != nil {
			return usageErr(err)
		}
		if *image && t.version != "" {
			return usageErr(fmt.Errorf("--image takes names without a version, not %q", arg))
		}
		targets = append(targets, t)
	}
	return a.locked("update", func() error {
		seed, err := a.seed()
		if err != nil {
			return sourceErr(err)
		}
		var res *updateResult
		if *lockDir != "" {
			res, err = a.updateFromDir(ctx, seed, *lockDir)
		} else {
			res, err = a.update(ctx, seed, targets, *image, *major)
		}
		if err != nil {
			return err
		}
		return a.p.Result(res, res.human)
	})
}

func (a *app) updateFromDir(ctx context.Context, seed *locksource.Lock, dir string) (*updateResult, error) {
	lock, err := locksource.FromDir(dir, locksource.Source{Origin: locksource.OriginDir, FetchedAt: a.cfg.Now().UTC()})
	if err != nil {
		return nil, sourceErr(err)
	}
	res, err := a.apply(ctx, lock, pins.Diff(lock.Deps, seed.Deps))
	if err != nil {
		return nil, err
	}
	res.Picked, res.Held = []pick{}, []pick{}
	a.afterAgents(ctx)
	return res, nil
}

// target is one NAME[@VERSION] argument; version may be empty or latest.
type target struct{ name, version string }

func parseTarget(arg string) (target, error) {
	name, version := arg, ""
	if i := strings.LastIndex(arg, "@"); i > 0 {
		name, version = arg[:i], arg[i+1:]
		if !pins.ValidVersion(version) {
			return target{}, fmt.Errorf("invalid version in %q", arg)
		}
	}
	if name == "" {
		return target{}, fmt.Errorf("invalid name %q", arg)
	}
	return target{name: name, version: version}, nil
}

// update moves the picked entities and converges both stores on the result.
func (a *app) update(ctx context.Context, seed *locksource.Lock, targets []target, image, major bool) (*updateResult, error) {
	cur, err := a.store.Current()
	if err != nil {
		return nil, output.Errorf(output.ExitFailure, "internal", "update", "%v", err)
	}
	agentOv, err := a.agentOverrides(seed, cur)
	if err != nil {
		return nil, output.Errorf(output.ExitFailure, "internal", "update", "%v", err)
	}
	all := status.AgentPins(seed, cur)
	var base map[string]string
	var toolOv pins.Overrides
	if a.toolsOn() {
		var toolPins []status.Pin
		base, toolPins, toolOv, err = a.toolsNow()
		if err != nil {
			return nil, output.Errorf(output.ExitFailure, "internal", "update", "base tools: %v", err)
		}
		all = append(all, toolPins...)
	}
	var orcaImage string
	var orcaOv pins.Overrides
	if a.orcaOn() {
		var orcaPin status.Pin
		if orcaImage, orcaPin, orcaOv, err = a.orcaNow(); err != nil {
			return nil, output.Errorf(output.ExitFailure, "internal", "update", "orca: %v", err)
		}
		all = append(all, orcaPin)
	}
	picked, held, err := a.picks(ctx, all, targets, image, major)
	if err != nil {
		return nil, err
	}
	for _, p := range picked {
		ov, pin := agentOv, seed.Deps[p.Name]
		switch p.Track {
		case status.TrackTools:
			ov, pin = toolOv, base[p.Name]
		case status.TrackOrca:
			ov, pin = orcaOv, orcaImage
		}
		if p.To == pin {
			delete(ov, p.Name)
		} else {
			ov[p.Name] = pins.Override{Version: p.To, Image: pin}
		}
	}
	res, err := a.convergeAgents(ctx, seed, cur, agentOv)
	if err != nil {
		return nil, err
	}
	if a.toolsOn() {
		if res.Tools, err = a.convergeTools(ctx, toolOv); err != nil {
			return nil, err
		}
	}
	if a.orcaOn() {
		if res.Orca, err = a.convergeOrca(ctx, orcaOv, false, true); err != nil {
			return nil, err
		}
	}
	res.Picked, res.Held = picked, held
	if res.Changed {
		a.afterAgents(ctx)
	} else {
		a.gcQuietly()
	}
	return res, nil
}

// picks decides what each entity moves to. With no targets, image returns
// every overridden entity to the image and otherwise every entity with an
// update moves, majors only with major; those are held instead.
func (a *app) picks(ctx context.Context, all []status.Pin, targets []target, image, major bool) (picked, held []pick, err error) {
	picked, held = []pick{}, []pick{}
	byName := map[string]status.Pin{}
	for _, p := range all {
		byName[p.Name] = p
	}
	if len(targets) == 0 {
		if image {
			for _, p := range all {
				if p.Current != p.Image {
					picked = append(picked, pick{Name: p.Name, Track: p.Track, From: p.Current, To: p.Image})
				}
			}
			return picked, held, nil
		}
		for _, p := range a.check(ctx, all) {
			switch {
			case p.Error != "":
				a.p.Logf("warning: %s: %s", p.Name, p.Error)
			case p.Update == "":
			case p.Update == pins.BumpMajor && !major:
				held = append(held, pick{Name: p.Name, Track: p.Track, From: p.Current, To: p.Latest, Bump: p.Update})
			default:
				picked = append(picked, pick{Name: p.Name, Track: p.Track, From: p.Current, To: p.Latest, Bump: p.Update})
			}
		}
		return picked, held, nil
	}
	for _, t := range targets {
		p, ok := byName[t.name]
		if !ok {
			return nil, nil, output.Errorf(output.ExitUsage, "unknown_name", "", "%s is not pinned in this image; berth status lists what is", t.name)
		}
		to := t.version
		switch {
		case image:
			to = p.Image
		case to == "" || to == latestKeyword:
			if to, err = a.latest(ctx, p); err != nil {
				return nil, nil, output.Errorf(output.ExitFailure, "fetch_failed", "resolve", "%v", err)
			}
		}
		if to != p.Current {
			picked = append(picked, pick{Name: p.Name, Track: p.Track, From: p.Current, To: to, Bump: pins.Bump(p.Current, to)})
		}
	}
	return picked, held, nil
}

// agentOverrides reads the agent set's overrides. A state from before
// overrides existed that does not follow the image gets them from cur.
func (a *app) agentOverrides(seed *locksource.Lock, cur *agentstore.Version) (pins.Overrides, error) {
	st, err := a.store.ReadState()
	if err != nil {
		return nil, err
	}
	if st.Overrides == nil && !st.FollowImage && cur != nil {
		return pins.Diff(cur.Manifest.Packages, seed.Deps), nil
	}
	return cloneOverrides(st.Overrides), nil
}

// toolsNow reads the base tools, their pins and the tool overrides.
func (a *app) toolsNow() (map[string]string, []status.Pin, pins.Overrides, error) {
	list, err := status.UserTools(a.cfg.BaseTools)
	if err != nil {
		return nil, nil, nil, err
	}
	base := map[string]string{}
	for _, t := range list {
		base[t.Name] = t.Version
	}
	ts := a.toolStore()
	cur, err := ts.Current()
	if err != nil {
		return nil, nil, nil, err
	}
	st, err := ts.ReadState()
	if err != nil {
		return nil, nil, nil, err
	}
	return base, status.ToolPins(list, cur), cloneOverrides(st.Overrides), nil
}

func cloneOverrides(o pins.Overrides) pins.Overrides {
	if o == nil {
		return pins.Overrides{}
	}
	return maps.Clone(o)
}

// convergeAgents makes the agent set seed with ov applied: the seed itself
// without overrides, the current set when it already holds those versions,
// and otherwise a set resolved against the registry.
func (a *app) convergeAgents(ctx context.Context, seed *locksource.Lock, cur *agentstore.Version, ov pins.Overrides) (*updateResult, error) {
	lock := seed
	if len(ov) > 0 {
		desired := pins.Desired(seed.Deps, ov)
		pkg, err := locksource.WithDeps(seed.PackageJSON, desired)
		if err != nil {
			return nil, sourceErr(err)
		}
		if cur != nil && maps.Equal(cur.Manifest.Packages, desired) {
			if err := a.installer().Refresh(cur, pkg); err != nil {
				return nil, output.Errorf(output.ExitFailure, "install_failed", "publish", "%v", err)
			}
			if err := a.store.WriteState(agentstore.State{Overrides: ov}); err != nil {
				return nil, output.Errorf(output.ExitFailure, "flip_failed", "flip", "%v", err)
			}
			c, p, err := a.views()
			if err != nil {
				return nil, output.Errorf(output.ExitFailure, "internal", "update", "%v", err)
			}
			return &updateResult{Current: c, Previous: p}, nil
		}
		if lock, err = a.installer().Resolve(ctx, seed, desired); err != nil {
			return nil, installErr(err)
		}
	}
	return a.apply(ctx, lock, ov)
}

func (a *app) installer() agentstore.Installer {
	return agentstore.Installer{
		Store: a.store, Runner: a.cfg.Runner, Smoke: a.cfg.Smoke,
		Agent: a.cfg.Agent, Now: a.cfg.Now, Logf: a.p.Logf,
	}
}

// apply installs lock if needed, flips to it and records ov.
func (a *app) apply(ctx context.Context, lock *locksource.Lock, ov pins.Overrides) (*updateResult, error) {
	v, installed, err := a.installer().Install(ctx, lock)
	if err != nil {
		return nil, installErr(err)
	}
	changed, err := a.store.Flip(v.Hash)
	if err != nil {
		return nil, output.Errorf(output.ExitFailure, "flip_failed", "flip", "%v", err)
	}
	if err := a.store.WriteState(agentstore.State{FollowImage: len(ov) == 0, Overrides: ov}); err != nil {
		return nil, output.Errorf(output.ExitFailure, "flip_failed", "flip", "%v", err)
	}
	a.rebuildLayer()
	cur, prev, err := a.views()
	if err != nil {
		return nil, output.Errorf(output.ExitFailure, "internal", "flip", "%v", err)
	}
	return &updateResult{Changed: changed, Installed: installed, FollowImage: len(ov) == 0, Current: cur, Previous: prev}, nil
}

// convergeTools makes the tool set hold exactly the tools in ov; with none,
// no set is current and every base tool is the image's.
func (a *app) convergeTools(ctx context.Context, ov pins.Overrides) (*trackResult, error) {
	ts := a.toolStore()
	if err := os.MkdirAll(ts.Root, 0o755); err != nil {
		return nil, output.Errorf(output.ExitFailure, "internal", "prepare", "%v", err)
	}
	res := &trackResult{}
	var err error
	if len(ov) == 0 {
		res.Changed, err = ts.Clear()
	} else {
		in := toolstore.Installer{Store: ts, Mise: a.cfg.Mise, Runner: a.cfg.Runner, Agent: a.cfg.Agent, Now: a.cfg.Now, Logf: a.p.Logf}
		var v *agentstore.Version
		if v, res.Installed, err = in.Install(ctx, ov.Versions()); err != nil {
			return nil, installErr(err)
		}
		res.Changed, err = ts.Flip(v.Hash)
	}
	if err != nil {
		return nil, output.Errorf(output.ExitFailure, "flip_failed", "flip", "%v", err)
	}
	if err := ts.WriteState(agentstore.State{FollowImage: len(ov) == 0, Overrides: ov}); err != nil {
		return nil, output.Errorf(output.ExitFailure, "flip_failed", "flip", "%v", err)
	}
	cur, err := ts.Current()
	if err != nil {
		return nil, output.Errorf(output.ExitFailure, "internal", "flip", "%v", err)
	}
	prev, err := ts.Previous()
	if err != nil {
		return nil, output.Errorf(output.ExitFailure, "internal", "flip", "%v", err)
	}
	res.Current, res.Previous = status.View(cur), status.View(prev)
	return res, nil
}

// afterAgents converges what depends on the agent set once it changed.
func (a *app) afterAgents(ctx context.Context) {
	a.setupMCP(ctx, a.cfg.MCPTimeout)
	a.setupSkills(ctx)
	a.gcQuietly()
}

type rollbackResult struct {
	Current  *status.VersionView `json:"current"`
	Previous *status.VersionView `json:"previous"`
}

func runRollback(ctx context.Context, a *app, args []string) error {
	fs := newFlagSet("rollback")
	tools := fs.Bool("tools", false, "roll back the base tools instead of the agents")
	orca := fs.Bool("orca", false, "roll back Orca instead of the agents")
	jsonOut := fs.Bool("json", false, "print JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return usageErr(err)
	}
	a.p.JSON = *jsonOut
	if len(pos) > 1 {
		return usageErr(fmt.Errorf("rollback takes at most one HASH"))
	}
	switch {
	case *tools && *orca:
		return usageErr(errors.New("--tools and --orca are mutually exclusive"))
	case *tools && !a.toolsOn():
		return usageErr(errors.New("this image has no base tools to roll back"))
	case *orca && !a.orcaOn():
		return usageErr(errors.New("this image has no Orca to roll back"))
	}
	track := status.TrackAgents
	switch {
	case *tools:
		track = status.TrackTools
	case *orca:
		track = status.TrackOrca
	}
	target := ""
	if len(pos) == 1 {
		target = pos[0]
	}
	return a.locked("rollback", func() error {
		store, label := a.store, "agent set"
		switch track {
		case status.TrackTools:
			store, label = a.toolStore(), "tool set"
		case status.TrackOrca:
			store, label = a.orcaStore(), "Orca version"
		}
		cur, err := store.Rollback(target)
		switch {
		case errors.Is(err, agentstore.ErrNoPrevious):
			return output.Errorf(output.ExitFailure, "no_previous", "rollback", "no previous %s to roll back to", label)
		case errors.Is(err, agentstore.ErrNotFound):
			return output.Errorf(output.ExitFailure, "not_found", "rollback", "no complete %s %q", label, target)
		case err != nil:
			return output.Errorf(output.ExitFailure, "flip_failed", "flip", "%v", err)
		}
		image, err := a.imagePins(track)
		if err != nil {
			return err
		}
		ov := pins.Diff(cur.Manifest.Packages, image)
		if err := store.WriteState(agentstore.State{FollowImage: len(ov) == 0, Overrides: ov}); err != nil {
			return output.Errorf(output.ExitFailure, "flip_failed", "flip", "%v", err)
		}
		switch track {
		case status.TrackAgents:
			a.rebuildLayer()
			a.setupMCP(ctx, a.cfg.MCPTimeout)
			a.setupSkills(ctx)
		case status.TrackOrca:
			a.restartOrca(ctx)
		}
		c, err := store.Current()
		if err != nil {
			return output.Errorf(output.ExitFailure, "internal", "rollback", "%v", err)
		}
		p, err := store.Previous()
		if err != nil {
			return output.Errorf(output.ExitFailure, "internal", "rollback", "%v", err)
		}
		res := rollbackResult{Current: status.View(c), Previous: status.View(p)}
		return a.p.Result(res, func(w io.Writer) {
			fmt.Fprintf(w, "rolled back to %s (previous: %s)\n", hashOrNone(res.Current), hashOrNone(res.Previous))
		})
	})
}

// imagePins are the image's versions of the agent packages, base tools or Orca.
func (a *app) imagePins(track string) (map[string]string, error) {
	if track == status.TrackOrca {
		return map[string]string{orcastore.Name: a.orcaImage()}, nil
	}
	if track == status.TrackAgents {
		seed, err := a.seed()
		if err != nil {
			return nil, sourceErr(err)
		}
		return seed.Deps, nil
	}
	base, _, _, err := a.toolsNow()
	if err != nil {
		return nil, output.Errorf(output.ExitFailure, "internal", "rollback", "base tools: %v", err)
	}
	return base, nil
}

// ensureTrack is what boot did to one store.
type ensureTrack struct {
	Action  string   `json:"action"`
	Reason  string   `json:"reason"`
	Dropped []string `json:"dropped"`
}

type ensureResult struct {
	Action   string              `json:"action"`
	Reason   string              `json:"reason"`
	SeedHash string              `json:"seed_hash"`
	Current  *status.VersionView `json:"current"`
	Dropped  []string            `json:"dropped"`
	Tools    *ensureTrack        `json:"tools,omitempty"`
	Orca     *ensureTrack        `json:"orca,omitempty"`
}

func runEnsure(ctx context.Context, a *app, args []string) error {
	fs := newFlagSet("ensure")
	jsonOut := fs.Bool("json", false, "print JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return usageErr(err)
	}
	a.p.JSON = *jsonOut
	if len(pos) > 0 {
		return usageErr(fmt.Errorf("ensure takes no arguments"))
	}
	return a.locked("ensure", func() error {
		seed, err := a.seed()
		if err != nil {
			return sourceErr(err)
		}
		res, err := a.ensureAgents(ctx, seed)
		if err != nil {
			return err
		}
		if a.toolsOn() {
			res.Tools = a.ensureTools(ctx)
		}
		if a.orcaOn() {
			res.Orca = a.ensureOrca(ctx)
		}
		// Added agents follow the image too; their converger runs as agent.
		a.syncAdded(ctx)
		a.rebuildLayer()
		if res.Current, _, err = a.views(); err != nil {
			return output.Errorf(output.ExitFailure, "internal", "ensure", "%v", err)
		}
		return a.p.Result(res, func(w io.Writer) {
			fmt.Fprintf(w, "agents %s (%s, %s)\n", hashOrNone(res.Current), res.Action, res.Reason)
			if res.Tools != nil {
				fmt.Fprintf(w, "base tools (%s, %s)\n", res.Tools.Action, res.Tools.Reason)
			}
			if res.Orca != nil {
				fmt.Fprintf(w, "orca (%s, %s)\n", res.Orca.Action, res.Orca.Reason)
			}
			for _, name := range slices.Concat(res.Dropped, toolsDropped(res.Tools), toolsDropped(res.Orca)) {
				fmt.Fprintf(w, "the image caught up with %s; it follows the image again\n", name)
			}
		})
	})
}

func toolsDropped(t *ensureTrack) []string {
	if t == nil {
		return nil
	}
	return t.Dropped
}

// ensureAgents brings the agent set in line with the image at boot: the
// seed, or the seed with the overrides a newer image has not caught up
// with. Without the registry it keeps a current set that has them.
func (a *app) ensureAgents(ctx context.Context, seed *locksource.Lock) (*ensureResult, error) {
	cur, err := a.store.Current()
	if err != nil {
		return nil, output.Errorf(output.ExitFailure, "internal", "ensure", "%v", err)
	}
	ov, err := a.agentOverrides(seed, cur)
	if err != nil {
		return nil, output.Errorf(output.ExitFailure, "internal", "ensure", "%v", err)
	}
	ov, dropped := pins.Rebase(ov, seed.Deps)
	res := &ensureResult{Action: "none", SeedHash: seed.Hash, Dropped: orEmpty(dropped)}
	if len(ov) == 0 {
		switch {
		case cur == nil:
			res.Reason = "no-current"
		case cur.Hash != seed.Hash:
			res.Reason = "seed-changed"
		case agentstore.PackageJSONStale(cur.Dir, seed.PackageJSON):
			res.Reason = "descriptors-changed"
		default:
			res.Reason = "up-to-date"
		}
		if res.Reason != "up-to-date" {
			if _, err := a.apply(ctx, seed, ov); err != nil {
				return nil, err
			}
			res.Action = "installed"
			if res.Reason == "descriptors-changed" {
				res.Action = "refreshed"
			}
			a.gcQuietly()
		} else if err := a.store.WriteState(agentstore.State{FollowImage: true}); err != nil {
			return nil, output.Errorf(output.ExitFailure, "flip_failed", "flip", "%v", err)
		}
		return res, nil
	}
	desired := pins.Desired(seed.Deps, ov)
	if cur != nil && maps.Equal(cur.Manifest.Packages, desired) {
		pkg, err := locksource.WithDeps(seed.PackageJSON, desired)
		if err != nil {
			return nil, sourceErr(err)
		}
		res.Reason = "pinned"
		if descriptorsStale(cur.Dir, pkg) {
			if err := a.installer().Refresh(cur, pkg); err != nil {
				return nil, output.Errorf(output.ExitFailure, "install_failed", "publish", "%v", err)
			}
			res.Action, res.Reason = "refreshed", "descriptors-changed"
		}
		if err := a.store.WriteState(agentstore.State{Overrides: ov}); err != nil {
			return nil, output.Errorf(output.ExitFailure, "flip_failed", "flip", "%v", err)
		}
		return res, nil
	}
	res.Reason = "overrides-changed"
	if cur == nil {
		res.Reason = "no-current"
	}
	if _, err := a.convergeAgents(ctx, seed, cur, ov); err != nil {
		if cur != nil {
			// Offline, say: keep what runs and try again at the next boot.
			a.p.Logf("warning: agents: %v; keeping the current set", err)
			res.Reason = "unresolved"
			return res, nil
		}
		a.p.Logf("warning: agents: %v; installing the image's set", err)
		if _, err := a.apply(ctx, seed, nil); err != nil {
			return nil, err
		}
		res.Action, res.Reason, res.Dropped = "installed", "no-current", slices.Sorted(maps.Keys(ov))
		a.gcQuietly()
		return res, nil
	}
	res.Action = "installed"
	a.gcQuietly()
	return res, nil
}

// descriptorsStale compares package.json by content: a set made from a
// lock directory names the same descriptors in another layout.
func descriptorsStale(dir string, pkg []byte) bool {
	raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return true
	}
	var have, want any
	if json.Unmarshal(raw, &have) != nil || json.Unmarshal(pkg, &want) != nil {
		return true
	}
	return !reflect.DeepEqual(have, want)
}

// ensureTools drops the tool overrides a newer image caught up with and
// converges the tool set. Failures are logged: base tools never block boot.
func (a *app) ensureTools(ctx context.Context) *ensureTrack {
	res := &ensureTrack{Action: "none", Reason: "up-to-date", Dropped: []string{}}
	base, _, ov, err := a.toolsNow()
	if err != nil {
		a.p.Logf("warning: base tools: %v", err)
		res.Reason = "unreadable"
		return res
	}
	ov, dropped := pins.Rebase(ov, base)
	res.Dropped = orEmpty(dropped)
	cur, err := a.toolStore().Current()
	if err != nil {
		a.p.Logf("warning: base tools: %v", err)
		res.Reason = "unreadable"
		return res
	}
	have := map[string]string{}
	if cur != nil {
		have = cur.Manifest.Packages
	}
	if maps.Equal(have, ov.Versions()) {
		return res
	}
	t, err := a.convergeTools(ctx, ov)
	if err != nil {
		a.p.Logf("warning: base tools: %v; keeping the current set", err)
		res.Reason = "unresolved"
		return res
	}
	res.Action, res.Reason = "installed", "overrides-changed"
	if t.Current == nil {
		res.Action = "cleared"
	}
	return res
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
