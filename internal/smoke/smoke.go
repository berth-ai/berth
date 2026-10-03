// Package smoke runs the post-install checks that gate publishing a version.
package smoke

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/berth-ai/bert/internal/runner"
)

const launchScript = `const { chromium } = require(process.argv[1]);
(async () => {
  const browser = await chromium.launch({ args: ['--no-sandbox'] });
  const page = await browser.newPage();
  await page.goto('about:blank');
  await browser.close();
})().catch((err) => { console.error(err); process.exit(1); });`

// Checker runs every check as the agent user with a per-check timeout.
type Checker struct {
	Runner  runner.Runner
	Timeout time.Duration
}

// Check verifies an unpublished version directory.
func (c Checker) Check(ctx context.Context, dir string, bins []string) error {
	for _, bin := range bins {
		if err := c.answers(ctx, dir, bin); err != nil {
			return err
		}
	}
	versions, err := PlaywrightCoreVersions(filepath.Join(dir, "node_modules"))
	if err != nil {
		return err
	}
	if len(versions) != 1 {
		return fmt.Errorf("expected exactly one playwright-core version, found %d: %s",
			len(versions), strings.Join(versions, ", "))
	}
	err = c.Runner.Run(ctx, runner.Cmd{
		Path:    "node",
		Args:    []string{"-e", launchScript, filepath.Join(dir, "node_modules", "playwright")},
		Env:     []string{"PLAYWRIGHT_BROWSERS_PATH=" + filepath.Join(dir, "ms-playwright")},
		AsAgent: true,
		Timeout: c.Timeout,
	})
	if err != nil {
		return fmt.Errorf("headless chromium did not launch: %w", err)
	}
	return nil
}

func (c Checker) answers(ctx context.Context, dir, bin string) error {
	var last error
	for _, flag := range []string{"--version", "--help"} {
		last = c.Runner.Run(ctx, runner.Cmd{
			Path: filepath.Join(dir, "bin", bin), Args: []string{flag}, AsAgent: true, Timeout: c.Timeout,
		})
		if last == nil {
			return nil
		}
	}
	return fmt.Errorf("%s answers neither --version nor --help: %w", bin, last)
}

// PlaywrightCoreVersions lists every distinct installed playwright-core version.
func PlaywrightCoreVersions(nodeModules string) ([]string, error) {
	seen := map[string]bool{}
	err := filepath.WalkDir(nodeModules, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || d.Name() != "playwright-core" || filepath.Base(filepath.Dir(p)) != "node_modules" {
			return nil
		}
		raw, err := os.ReadFile(filepath.Join(p, "package.json"))
		if err != nil {
			return err
		}
		var pkg struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(raw, &pkg); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		seen[pkg.Version] = true
		return nil
	})
	versions := make([]string, 0, len(seen))
	for v := range seen {
		versions = append(versions, v)
	}
	slices.Sort(versions)
	return versions, err
}
