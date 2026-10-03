package skills

import (
	"strings"
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	cases := map[string]struct {
		in   string
		name string
		desc string
	}{
		"plain":         {"---\nname: pdf\ndescription: Read PDF files\n---\n# Body\n", "pdf", "Read PDF files"},
		"crlf and bom":  {"\ufeff---\r\nname: pdf\r\ndescription: x\r\n---\r\n", "pdf", "x"},
		"double quoted": {"---\nname: \"pdf\"\ndescription: \"Say \\\"hi\\\"\"\n---\n", "pdf", `Say "hi"`},
		"single quoted": {"---\nname: 'pdf'\ndescription: 'It''s fine'\n---\n", "pdf", "It's fine"},
		"folded":        {"---\nname: pdf\ndescription: >\n  Read and\n  write PDFs.\n---\n", "pdf", "Read and write PDFs."},
		"literal":       {"---\nname: pdf\ndescription: |-\n  line one\n  line two\n---\n", "pdf", "line one\nline two"},
		"plain wrapped": {"---\nname: pdf\ndescription: Read and\n  write PDFs.\n---\n", "pdf", "Read and write PDFs."},
		"comment":       {"---\nname: pdf # the id\ndescription: x\n---\n", "pdf", "x"},
		"other keys":    {"---\nlicense: MIT\nmetadata:\n  name: nested\nname: pdf\ndescription: x\n---\n", "pdf", "x"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m, err := ParseFrontmatter([]byte(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			if m.Name != tc.name || m.Description != tc.desc {
				t.Errorf("got %+v", m)
			}
		})
	}
}

func TestParseFrontmatterRejects(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"no block":       {"# Title\n", "does not start with"},
		"unclosed":       {"---\nname: pdf\n", "no closing"},
		"no name":        {"---\ndescription: x\n---\n", "has no name"},
		"bad name":       {"---\nname: My Skill\ndescription: x\n---\n", "lowercase"},
		"no description": {"---\nname: pdf\n---\n", "has no description"},
		"long":           {"---\nname: pdf\ndescription: " + strings.Repeat("a", 1025) + "\n---\n", "longer than 1024"},
		"bad quote":      {"---\nname: pdf\ndescription: \"open\n---\n", "malformed"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseFrontmatter([]byte(tc.in)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
}
