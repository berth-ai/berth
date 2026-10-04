// Package skills installs agent skills: folders holding a SKILL.md, taken
// from git repositories and pinned to a commit. berth skills scan lists and
// checks the skills at a ref; berth skills sync exports every configured
// skill once and links it into the agents' skills directories.
package skills

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/berth-ai/berth/internal/config"
)

// Meta is what Berth reads from a SKILL.md frontmatter.
type Meta struct {
	Name        string
	Description string
}

// ParseFrontmatter reads name and description from SKILL.md's YAML
// frontmatter. It handles the scalar forms skills use (plain, quoted, and
// | or > blocks) and skips every other key, nested ones included.
func ParseFrontmatter(data []byte) (Meta, error) {
	text := strings.TrimPrefix(strings.ReplaceAll(string(data), "\r\n", "\n"), "\ufeff")
	lines := strings.Split(text, "\n")
	if strings.TrimRight(lines[0], " ") != "---" {
		return Meta{}, errors.New("SKILL.md does not start with a --- frontmatter block")
	}
	var block []string
	closed := false
	for _, l := range lines[1:] {
		if strings.TrimRight(l, " ") == "---" {
			closed = true
			break
		}
		block = append(block, l)
	}
	if !closed {
		return Meta{}, errors.New("SKILL.md frontmatter has no closing ---")
	}
	vals := map[string]string{}
	for i := 0; i < len(block); i++ {
		l := block[i]
		if l == "" || indented(l) || strings.HasPrefix(l, "#") {
			continue
		}
		key, val, ok := strings.Cut(l, ":")
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		var cont []string
		for i+1 < len(block) && (block[i+1] == "" || indented(block[i+1])) {
			i++
			cont = append(cont, block[i])
		}
		if !ok || (key != "name" && key != "description") {
			continue
		}
		s, err := scalar(val, cont)
		if err != nil {
			return Meta{}, fmt.Errorf("SKILL.md %s %w", key, err)
		}
		vals[key] = s
	}
	m := Meta{Name: vals["name"], Description: vals["description"]}
	switch {
	case m.Name == "":
		return m, errors.New("SKILL.md has no name")
	case !config.SkillIDPattern.MatchString(m.Name):
		return m, fmt.Errorf("SKILL.md name %q must be 1 to 64 lowercase letters, digits and dashes", m.Name)
	case m.Description == "":
		return m, errors.New("SKILL.md has no description")
	case utf8.RuneCountInString(m.Description) > 1024:
		return m, errors.New("SKILL.md description is longer than 1024 characters")
	}
	return m, nil
}

func indented(l string) bool { return l[0] == ' ' || l[0] == '\t' }

func scalar(val string, cont []string) (string, error) {
	trimmed := make([]string, 0, len(cont))
	for _, c := range cont {
		trimmed = append(trimmed, strings.TrimSpace(c))
	}
	switch {
	case strings.HasPrefix(val, "|"):
		return strings.TrimRight(strings.Join(trimmed, "\n"), "\n"), nil
	case strings.HasPrefix(val, ">"):
		return fold(trimmed), nil
	case strings.HasPrefix(val, `"`):
		s, err := strconv.Unquote(strings.Join(append([]string{val}, trimmed...), " "))
		if err != nil {
			return "", errors.New("is a malformed double-quoted string")
		}
		return s, nil
	case strings.HasPrefix(val, "'"):
		s := strings.Join(append([]string{val}, trimmed...), " ")
		if len(s) < 2 || !strings.HasSuffix(s, "'") {
			return "", errors.New("is a malformed single-quoted string")
		}
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'"), nil
	}
	s := fold(append([]string{val}, trimmed...))
	if i := strings.Index(s, " #"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s), nil
}

// fold joins lines with spaces; an empty line becomes a line break.
func fold(lines []string) string {
	var b strings.Builder
	for _, l := range lines {
		switch {
		case l == "":
			b.WriteString("\n")
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "\n"):
			b.WriteString(" " + l)
		default:
			b.WriteString(l)
		}
	}
	return strings.TrimSpace(b.String())
}
