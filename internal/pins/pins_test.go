package pins

import (
	"maps"
	"slices"
	"testing"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.2.3", "1.2.10", -1},
		{"1.10.0", "1.9.9", 1},
		{"v1.2.3", "1.2.3", 0},
		{"1.2", "1.2.0", 0},
		{"1.2.3-beta.1", "1.2.3", -1},
		{"1.2.3-beta.2", "1.2.3-beta.10", -1},
		{"1.2.3-alpha", "1.2.3-beta", -1},
		{"1.2.3-1", "1.2.3-alpha", -1},
		{"1.2.3-beta", "1.2.3-beta.1", -1},
		{"1.2.3+build.5", "1.2.3", 0},
		{"2026.11.5", "2026.2.0", 1},
		{"0.0.85", "0.0.9", 1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := Compare(c.b, c.a); got != -c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.b, c.a, got, -c.want)
		}
	}
}

func TestBump(t *testing.T) {
	cases := []struct{ from, to, want string }{
		{"1.2.3", "1.2.4", BumpPatch},
		{"1.2.3", "1.3.0", BumpMinor},
		{"1.2.3", "2.0.0", BumpMajor},
		{"0.167.0", "0.170.1", BumpMinor},
		{"1.2.3", "1.2.3", ""},
		{"1.2.3", "1.2.2", ""},
		{"1.2.3-rc.1", "1.2.3", BumpPatch},
		{"2026.11.5", "2027.1.0", BumpMajor},
	}
	for _, c := range cases {
		if got := Bump(c.from, c.to); got != c.want {
			t.Errorf("Bump(%q, %q) = %q, want %q", c.from, c.to, got, c.want)
		}
	}
}

func TestStable(t *testing.T) {
	if !Stable("1.2.3") || Stable("1.2.3-next.4") || !Stable("1.2.3+meta") {
		t.Fatal("Stable misclassifies")
	}
}

func TestRebase(t *testing.T) {
	o := Overrides{
		"kept":       {Version: "1.5.0", Image: "1.0.0"}, // image unchanged
		"moved":      {Version: "1.5.0", Image: "1.0.0"}, // image moved, still older
		"caught":     {Version: "1.5.0", Image: "1.0.0"}, // image caught up
		"passed":     {Version: "1.5.0", Image: "1.0.0"}, // image overtook it
		"downgrade":  {Version: "0.9.0", Image: "1.0.0"}, // rolled back, image moved
		"rolledback": {Version: "0.9.0", Image: "1.0.0"}, // rolled back, image unchanged
		"gone":       {Version: "1.5.0", Image: "1.0.0"}, // image dropped it
		"same":       {Version: "1.0.0", Image: "0.9.0"}, // equals the pin
	}
	image := map[string]string{
		"kept": "1.0.0", "moved": "1.2.0", "caught": "1.5.0", "passed": "2.0.0",
		"downgrade": "1.1.0", "rolledback": "1.0.0", "same": "1.0.0",
	}
	kept, dropped := Rebase(o, image)
	want := Overrides{
		"kept":       {Version: "1.5.0", Image: "1.0.0"},
		"moved":      {Version: "1.5.0", Image: "1.2.0"},
		"rolledback": {Version: "0.9.0", Image: "1.0.0"},
	}
	if !maps.Equal(kept, want) {
		t.Errorf("kept = %v, want %v", kept, want)
	}
	if want := []string{"caught", "downgrade", "gone", "passed", "same"}; !slices.Equal(dropped, want) {
		t.Errorf("dropped = %v, want %v", dropped, want)
	}
}

func TestDesiredAndDiff(t *testing.T) {
	image := map[string]string{"a": "1.0.0", "b": "2.0.0"}
	o := Overrides{"a": {Version: "1.1.0", Image: "1.0.0"}, "zz": {Version: "9.0.0", Image: "8.0.0"}}
	got := Desired(image, o)
	if want := map[string]string{"a": "1.1.0", "b": "2.0.0"}; !maps.Equal(got, want) {
		t.Errorf("Desired = %v, want %v", got, want)
	}
	if image["a"] != "1.0.0" {
		t.Error("Desired changed its input")
	}
	diff := Diff(map[string]string{"a": "1.1.0", "b": "2.0.0", "c": "3.0.0"}, image)
	if want := (Overrides{"a": {Version: "1.1.0", Image: "1.0.0"}}); !maps.Equal(diff, want) {
		t.Errorf("Diff = %v, want %v", diff, want)
	}
	if v := o.Versions(); v["a"] != "1.1.0" || len(v) != 2 {
		t.Errorf("Versions = %v", v)
	}
}

func TestValidVersion(t *testing.T) {
	for _, v := range []string{"1.2.3", "v1.2.3", "2026.11.5", "1.0.0-beta.1+x"} {
		if !ValidVersion(v) {
			t.Errorf("ValidVersion(%q) = false", v)
		}
	}
	for _, v := range []string{"", "latest;rm", "1 2", "-1", "../x"} {
		if ValidVersion(v) {
			t.Errorf("ValidVersion(%q) = true", v)
		}
	}
}
