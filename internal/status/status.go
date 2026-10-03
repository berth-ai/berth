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

// Check is the result of comparing current with the lock on a ref.
type Check struct {
	Ref             string `json:"ref"`
	Commit          string `json:"commit"`
	Hash            string `json:"hash"`
	UpdateAvailable bool   `json:"update_available"`
}

// Report is the full status.
type Report struct {
	Current     *VersionView    `json:"current"`
	Previous    *VersionView    `json:"previous"`
	Versions    []VersionView   `json:"versions"`
	FollowImage bool            `json:"follow_image"`
	Seed        *Seed           `json:"seed"`
	UserTools   []Tool          `json:"user_tools"`
	Build       json.RawMessage `json:"build"`
	Check       *Check          `json:"check,omitempty"`
	Warnings    []string        `json:"warnings"`
}

// Inputs are the paths Collect reads.
type Inputs struct {
	Store      agentstore.Store
	SeedDir    string
	BuildInfo  string
	MiseConfig string
}

// Collect reads the store, seed, build info and user tools. Problems with
// anything but the store become warnings, so status always answers.
func Collect(in Inputs) (Report, error) {
	rep := Report{UserTools: []Tool{}, Warnings: []string{}, Versions: []VersionView{}}
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
	if r.Check != nil {
		if r.Check.UpdateAvailable {
			fmt.Fprintf(w, "update    available: %s (%s@%.7s); run berth update\n", r.Check.Hash, r.Check.Ref, r.Check.Commit)
		} else {
			fmt.Fprintf(w, "update    up to date with %s\n", r.Check.Ref)
		}
	}
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
	case locksource.OriginRef:
		return fmt.Sprintf("%s@%s (%.7s)", s.Repo, s.Ref, s.Commit)
	case locksource.OriginDir:
		return "local directory"
	default:
		return string(s.Origin)
	}
}
