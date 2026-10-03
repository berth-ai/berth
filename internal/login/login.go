// Package login keeps the panel's single login: an argon2id hash in
// /var/lib/berth/secrets/panel.json that only root reads, and the one-time
// setup token that creates it.
package login

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/crypto/argon2"

	"github.com/berth-ai/berth/internal/config"
)

const (
	argonTime    = 1
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 4
	argonKeyLen  = 32
	saltLen      = 16
)

var userPattern = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,64}$`)

// ValidUser checks a login name.
func ValidUser(user string) error {
	if !userPattern.MatchString(user) {
		return errors.New("the user name must be 1 to 64 letters, digits or . _ @ -")
	}
	return nil
}

// ValidPassword checks a new password.
func ValidPassword(password string) error {
	if len(password) < 10 || len(password) > 256 {
		return errors.New("the password must be 10 to 256 characters long")
	}
	if strings.IndexFunc(password, unicode.IsControl) >= 0 {
		return errors.New("the password must not contain control characters")
	}
	return nil
}

var b64 = base64.RawStdEncoding

// Hash returns an argon2id PHC string for password.
func Hash(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// Check reports whether password matches an argon2id PHC string, comparing
// in constant time. Parameters are bounded so a tampered file cannot make a
// check exhaust memory.
func Check(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, errors.New("not an argon2id hash")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, fmt.Errorf("unsupported argon2 version %q", parts[2])
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, fmt.Errorf("invalid argon2 parameters %q", parts[3])
	}
	if m < 8 || m > 1<<20 || t < 1 || t > 10 || p < 1 || p > 16 {
		return false, fmt.Errorf("argon2 parameters out of range: %q", parts[3])
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("invalid salt: %w", err)
	}
	key, err := b64.DecodeString(parts[5])
	if err != nil || len(key) < 16 || len(key) > 64 {
		return false, errors.New("invalid key")
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(key)))
	return subtle.ConstantTimeCompare(got, key) == 1, nil
}

// Store is panel.json.
type Store struct {
	Path string
}

type record struct {
	User string `json:"user"`
	Hash string `json:"hash"`
}

// Exists reports whether a login has been created.
func (s Store) Exists() (bool, error) {
	_, err := os.Stat(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// User returns the login name.
func (s Store) User() (string, error) {
	rec, err := s.read()
	return rec.User, err
}

// Set validates and stores a new login, replacing any old one.
func (s Store) Set(user, password string) error {
	if err := ValidUser(user); err != nil {
		return err
	}
	if err := ValidPassword(password); err != nil {
		return err
	}
	hash, err := Hash(password)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(record{User: user, Hash: hash}, "", "  ")
	if err != nil {
		return err
	}
	// 0711: agent may open agents.env in here by name but never list it;
	// panel.json itself is 0600.
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o711); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(s.Path), 0o711); err != nil {
		return err
	}
	return config.WriteFileAtomic(s.Path, append(raw, '\n'), 0o600, -1, -1)
}

// Verify checks user and password. It always computes the hash, so a wrong
// user name takes as long as a wrong password.
func (s Store) Verify(user, password string) (bool, error) {
	rec, err := s.read()
	if err != nil {
		return false, err
	}
	want, got := sha256.Sum256([]byte(rec.User)), sha256.Sum256([]byte(user))
	userOK := subtle.ConstantTimeCompare(want[:], got[:]) == 1
	passOK, err := Check(rec.Hash, password)
	if err != nil {
		return false, err
	}
	return userOK && passOK, nil
}

// Delete removes the login; a missing login is not an error.
func (s Store) Delete() error {
	if err := os.Remove(s.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (s Store) read() (record, error) {
	var rec record
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		return rec, err
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		return rec, fmt.Errorf("%s: %w", s.Path, err)
	}
	return rec, nil
}

// NewToken returns a fresh 128-bit setup token in hex.
func NewToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on Linux
	}
	return hex.EncodeToString(b)
}

// TokenEqual compares a presented token with the expected one in constant
// time. An empty expected token (no setup in progress) never matches.
func TokenEqual(want, got string) bool {
	if want == "" {
		return false
	}
	w, g := sha256.Sum256([]byte(want)), sha256.Sum256([]byte(got))
	return subtle.ConstantTimeCompare(w[:], g[:]) == 1
}
