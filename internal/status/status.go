// Package status builds the report behind `berth status`.
package status

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/berth-ai/berth/internal/agentstore"
	"github.com/berth-ai/berth/internal/locksource"
	"github.com/berth-ai/berth/internal/orcastore"
	"github.com/berth-ai/berth/internal/pins"
)

// VersionView is the JSON shape of an installed agent set.
type VersionView struct {
	Hash        string            `json:"hash"`
	Source      locksource.Source `json:"source"`
	Packages    map[string]string `json:"packages"`
	InstalledAt time.Time         `json:"installed_at"`
}

// View converts a version; nil stays nil.
func View(v *agentstore.Version) *VersionView {
	if v == nil {
		return nil
	}
	return &VersionView{Hash: v.Hash, Source: v.Source, Packages: v.Manifest.Packages, InstalledAt: v.Manifest.InstalledAt}
}

// Tool is one user-track mise tool.
type Tool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Seed describes the lock baked into the image.
type Seed struct {
	Hash string `json:"hash"`
}

// Tracks: the agent set, the base tools and Orca.
const (
	TrackAgents = "agents"
	TrackTools  = "tools"
	TrackOrca   = "orca"
)

// KindTool is the kind of a base tool and KindFrontend Orca's; agent
// packages use the lock's kinds.
const (
	KindTool     = "tool"
	KindFrontend = "frontend"
)

// Pin is one entity the image pins: its version in the image, the version
// that runs, and, after a check, the newest version upstream.
type Pin struct {
	Name    string `json:"name"`
	Track   string `json:"track"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Image   string `json:"image"`
	Current string `json:"current"`
	Latest  string `json:"latest,omitempty"`
	Update  string `json:"update,omitempty"` // major, minor or patch from current to latest
	Error   string `json:"error,omitempty"`  // why the check found no latest
}

// ToolsReport is the base-tools store: the sets that override the image's
// pins. No current set means every base tool is the image's.
type ToolsReport struct {
	Current  *VersionView `json:"current"`
	Previous *VersionView `json:"previous"`
}

// Report is the full status.
type Report struct {
	Current     *VersionView    `json:"current"`
	Previous    *VersionView    `json:"previous"`
	Versions    []VersionView   `json:"versions"`
	FollowImage bool            `json:"follow_image"`
	Seed        *Seed           `json:"seed"`
	Pins        []Pin           `json:"pins"`
	Tools       ToolsReport     `json:"tools"`
	Orca        *ToolsReport    `json:"orca,omitempty"` // absent in an image without Orca
	CheckedAt   *time.Time      `json:"checked_at,omitempty"`
	UserTools   []Tool          `json:"user_tools"`
	Build       json.RawMessage `json:"build"`
	Warnings    []string        `json:"warnings"`
}

// Inputs are the paths Collect reads. A zero ToolStore or an empty
// BaseTools leaves the base tools out; a zero OrcaStore or an empty
// OrcaImage leaves Orca out.
type Inputs struct {
	Store      agentstore.Store
	ToolStore  agentstore.Store
	OrcaStore  agentstore.Store
	OrcaImage  string // the image's Orca release
	SeedDir    string
	BaseTools  string
	BuildInfo  string
	MiseConfig string
}

// Collect reads the store, seed, build info and user tools. Problems with
// anything but the store become warnings, so status always answers.
func Collect(in Inputs) (Report, error) {
	rep := Report{UserTools: []Tool{}, Warnings: []string{}, Versions: []VersionView{}, Pins: []Pin{}}
	cur, err := in.Store.Current()
	if err != nil {
		return rep, err
	}
	prev, err := in.Store.Previous()
	if err != nil {
		return rep, err
	}
	all, err := in.Store.List()
	if err != nil {
		return rep, err
	}
	for i := range all {
		rep.Versions = append(rep.Versions, *View(&all[i]))
	}
	state, err := in.Store.ReadState()
	if err != nil {
		return rep, err
	}
	rep.Current, rep.Previous, rep.FollowImage = View(cur), View(prev), state.FollowImage
	if seed, err := locksource.FromDir(in.SeedDir, locksource.Source{Origin: locksource.OriginImage}); err != nil {
		rep.Warnings = append(rep.Warnings, "image seed: "+err.Error())
	} else {
		rep.Seed = &Seed{Hash: seed.Hash}
		rep.Pins = append(rep.Pins, AgentPins(seed, cur)...)
	}
	if in.ToolStore.Root != "" && in.BaseTools != "" {
		if err := collectTools(&rep, in); err != nil {
			rep.Warnings = append(rep.Warnings, "base tools: "+err.Error())
		}
	}
	if in.OrcaStore.Root != "" && in.OrcaImage != "" {
		if err := collectOrca(&rep, in); err != nil {
			rep.Warnings = append(rep.Warnings, "orca: "+err.Error())
		}
	}
	if raw, err := os.ReadFile(in.BuildInfo); err == nil && json.Valid(raw) {
		rep.Build = raw
	}
	tools, err := UserTools(in.MiseConfig)
	if err != nil {
		rep.Warnings = append(rep.Warnings, "user tools: "+err.Error())
	}
	rep.UserTools = tools
	return rep, nil
}

// AgentPins describes the packages of seed against the current set cur.
func AgentPins(seed *locksource.Lock, cur *agentstore.Version) []Pin {
	out := []Pin{}
	for _, name := range slices.Sorted(maps.Keys(seed.Deps)) {
		m, ok := seed.Meta[name]
		if !ok {
			continue // a dependency the lock does not describe, such as playwright
		}
		p := Pin{Name: name, Track: TrackAgents, Kind: m.Kind, Title: m.Name, Image: seed.Deps[name], Current: seed.Deps[name]}
		if cur != nil {
			if v, ok := cur.Manifest.Packages[name]; ok {
				p.Current = v
			}
		}
		out = append(out, p)
	}
	return out
}

// ToolPins describes the base tools against the current tool set cur.
func ToolPins(base []Tool, cur *agentstore.Version) []Pin {
	out := []Pin{}
	for _, t := range base {
		p := Pin{Name: t.Name, Track: TrackTools, Kind: KindTool, Title: t.Name, Image: t.Version, Current: t.Version}
		if cur != nil {
			if v, ok := cur.Manifest.Packages[t.Name]; ok {
				p.Current = v
			}
		}
		out = append(out, p)
	}
	return out
}

// OrcaPin describes Orca: the image's release against the current Orca
// version cur. With none installed, Current is the release Orca would
// install: the override in ov, or the image's.
func OrcaPin(image string, cur *agentstore.Version, ov pins.Overrides) Pin {
	p := Pin{Name: orcastore.Name, Track: TrackOrca, Kind: KindFrontend, Title: orcastore.Title, Image: image, Current: image}
	if o, ok := ov[orcastore.Name]; ok {
		p.Current = o.Version
	}
	if cur != nil {
		if v, ok := cur.Manifest.Packages[orcastore.Name]; ok {
			p.Current = v
		}
	}
	return p
}

func collectOrca(rep *Report, in Inputs) error {
	cur, err := in.OrcaStore.Current()
	if err != nil {
		return err
	}
	prev, err := in.OrcaStore.Previous()
	if err != nil {
		return err
	}
	st, err := in.OrcaStore.ReadState()
	if err != nil {
		return err
	}
	rep.Orca = &ToolsReport{Current: View(cur), Previous: View(prev)}
	rep.Pins = append(rep.Pins, OrcaPin(in.OrcaImage, cur, st.Overrides))
	return nil
}

func collectTools(rep *Report, in Inputs) error {
	cur, err := in.ToolStore.Current()
	if err != nil {
		return err
	}
	prev, err := in.ToolStore.Previous()
	if err != nil {
		return err
	}
	rep.Tools = ToolsReport{Current: View(cur), Previous: View(prev)}
	base, err := UserTools(in.BaseTools)
	if err != nil {
		return err
	}
	rep.Pins = append(rep.Pins, ToolPins(base, cur)...)
	return nil
}

// UserTools lists the [tools] of a mise config; a missing file is empty.
func UserTools(path string) ([]Tool, error) {
	tools := []Tool{}
	if path == "" {
		return tools, nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return tools, nil
	}
	if err != nil {
		return tools, err
	}
	var cfg struct {
		Tools map[string]any `toml:"tools"`
	}
	if err := toml.Unmarshal(raw, &cfg); err != nil {
		return tools, fmt.Errorf("%s: %w", path, err)
	}
	for _, name := range slices.Sorted(maps.Keys(cfg.Tools)) {
		tools = append(tools, Tool{Name: name, Version: toolVersion(cfg.Tools[name])})
	}
	return tools, nil
}

func toolVersion(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			parts = append(parts, toolVersion(e))
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		if s, ok := t["version"].(string); ok {
			return s
		}
	}
	return fmt.Sprint(v)
}

// WriteHuman renders the report for a terminal.
func WriteHuman(w io.Writer, r Report) {
	writeVersion(w, "current", r.Current)
	writeVersion(w, "previous", r.Previous)
	mode := "pinned (restarts keep the current agent set)"
	if r.FollowImage {
		mode = "following the image seed"
	}
	fmt.Fprintf(w, "mode      %s\n", mode)
	if r.Seed != nil {
		fmt.Fprintf(w, "seed      %s\n", r.Seed.Hash)
	}
	writePins(w, r)
	var b struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
		Arch    string `json:"arch"`
	}
	if json.Unmarshal(r.Build, &b) == nil && b.Version != "" {
		fmt.Fprintf(w, "image     berth %s (%.7s) %s\n", b.Version, b.Commit, b.Arch)
	}
	fmt.Fprintln(w, "user tools")
	if len(r.UserTools) == 0 {
		fmt.Fprintln(w, "  none")
	}
	for _, t := range r.UserTools {
		fmt.Fprintf(w, "  %s %s\n", t.Name, t.Version)
	}
	for _, warning := range r.Warnings {
		fmt.Fprintf(w, "warning   %s\n", warning)
	}
}

// writePins lists the pinned entities that differ from the image or, after
// a check, have an update or failed; a check also counts the updates.
func writePins(w io.Writer, r Report) {
	updates, header := 0, false
	for _, p := range r.Pins {
		if p.Current == p.Image && p.Update == "" && p.Error == "" {
			continue
		}
		if !header {
			fmt.Fprintln(w, "pins")
			header = true
		}
		line := fmt.Sprintf("  %s %s", p.Name, p.Current)
		if p.Current != p.Image {
			line += fmt.Sprintf(" (image %s)", p.Image)
		}
		switch {
		case p.Update != "":
			updates++
			line += fmt.Sprintf(" -> %s (%s)", p.Latest, p.Update)
		case p.Error != "":
			line += " (check failed: " + p.Error + ")"
		}
		fmt.Fprintln(w, line)
	}
	switch {
	case r.CheckedAt == nil:
	case updates == 0:
		fmt.Fprintln(w, "updates   none; everything is at its newest version")
	default:
		fmt.Fprintf(w, "updates   %d available; run berth update, or berth update NAME for a major\n", updates)
	}
}

func writeVersion(w io.Writer, label string, v *VersionView) {
	if v == nil {
		fmt.Fprintf(w, "%-9s none\n", label)
		return
	}
	fmt.Fprintf(w, "%-9s %s  %s  installed %s\n", label, v.Hash, describe(v.Source), v.InstalledAt.UTC().Format("2006-01-02 15:04 MST"))
	for _, name := range slices.Sorted(maps.Keys(v.Packages)) {
		fmt.Fprintf(w, "  %s %s\n", name, v.Packages[name])
	}
}

func describe(s locksource.Source) string {
	switch s.Origin {
	case locksource.OriginUpstream:
		return "upstream"
	case locksource.OriginRef:
		return fmt.Sprintf("%s@%s (%.7s)", s.Repo, s.Ref, s.Commit)
	case locksource.OriginDir:
		return "local directory"
	default:
		return string(s.Origin)
	}
}
