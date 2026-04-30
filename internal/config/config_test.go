package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func validConfig() *Config {
	c := &Config{
		Users: []string{"alice", "bob"},
		Shares: map[string]Share{
			"docs": {
				Path: "/data/docs",
				Access: map[string]string{
					"alice": "rw",
					"bob":   "ro",
				},
			},
		},
	}
	c.Server.ApplyDefaults()
	return c
}

func TestValidate_Valid(t *testing.T) {
	if err := Validate(validConfig(), Passwords{"alice": "x", "bob": "y"}); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
}

func TestValidate_AccessForUndeclaredUser(t *testing.T) {
	c := validConfig()
	c.Shares["docs"].Access["carol"] = "rw"
	err := Validate(c, Passwords{})
	if err == nil || !strings.Contains(err.Error(), `user "carol" not declared`) {
		t.Fatalf("expected undeclared-user error, got: %v", err)
	}
}

func TestValidate_GuestAndAccessConflict(t *testing.T) {
	c := validConfig()
	sh := c.Shares["docs"]
	sh.Guest = true
	c.Shares["docs"] = sh
	err := Validate(c, Passwords{})
	if err == nil || !strings.Contains(err.Error(), "cannot have both guest=true and access list") {
		t.Fatalf("expected guest+access conflict, got: %v", err)
	}
}

func TestValidate_NoSharesDeclared(t *testing.T) {
	c := validConfig()
	c.Shares = map[string]Share{}
	err := Validate(c, Passwords{})
	if err == nil || !strings.Contains(err.Error(), "no shares declared") {
		t.Fatalf("expected no-shares error, got: %v", err)
	}
}

func TestValidate_RelativePath(t *testing.T) {
	c := validConfig()
	sh := c.Shares["docs"]
	sh.Path = "data/docs"
	c.Shares["docs"] = sh
	err := Validate(c, Passwords{})
	if err == nil || !strings.Contains(err.Error(), "absolute path") {
		t.Fatalf("expected absolute-path error, got: %v", err)
	}
}

func TestValidate_BadAccessLevel(t *testing.T) {
	c := validConfig()
	c.Shares["docs"].Access["alice"] = "wat"
	err := Validate(c, Passwords{})
	if err == nil || !strings.Contains(err.Error(), `access level "wat" invalid`) {
		t.Fatalf("expected bad-access-level error, got: %v", err)
	}
}

func TestValidate_PasswordForUndeclaredUser(t *testing.T) {
	c := validConfig()
	err := Validate(c, Passwords{"carol": "secret"})
	if err == nil || !strings.Contains(err.Error(), `non-declared user "carol"`) {
		t.Fatalf("expected non-declared-user error, got: %v", err)
	}
}

func TestValidate_DuplicateUser(t *testing.T) {
	c := validConfig()
	c.Users = append(c.Users, "alice")
	err := Validate(c, Passwords{})
	if err == nil || !strings.Contains(err.Error(), "declared twice") {
		t.Fatalf("expected duplicate-user error, got: %v", err)
	}
}

func TestMissingPasswords(t *testing.T) {
	c := validConfig()
	missing := MissingPasswords(c, Passwords{"alice": "x"})
	if len(missing) != 1 || missing[0] != "bob" {
		t.Fatalf("expected [bob], got %v", missing)
	}
}

func TestApplyDefaults(t *testing.T) {
	s := Server{}
	s.ApplyDefaults()
	if s.Workgroup != DefaultWorkgroup ||
		s.NetbiosName != DefaultNetbiosName ||
		s.DataUID != DefaultDataUID ||
		s.MinProtocol != DefaultMinProto {
		t.Fatalf("defaults not applied: %+v", s)
	}
	if !s.WSDEnabled() {
		t.Fatal("WSD should default to enabled when unset in YAML")
	}
}

func TestWSDExplicitFalse(t *testing.T) {
	f := false
	s := Server{WSD: &f}
	s.ApplyDefaults()
	if s.WSDEnabled() {
		t.Fatal("explicit wsd:false should stay disabled after ApplyDefaults")
	}
}

func TestNormalizeAccessCase(t *testing.T) {
	// Loaded YAML may have RW instead of rw.
	c := &Config{
		Users: []string{"alice"},
		Shares: map[string]Share{
			"docs": {Path: "/data/docs", Access: map[string]string{"alice": "RW"}},
		},
	}
	c.Server.ApplyDefaults()
	c.normalize()
	if c.Shares["docs"].Access["alice"] != "rw" {
		t.Fatalf("expected normalized 'rw', got %q", c.Shares["docs"].Access["alice"])
	}
}

// Sanity check that absolute paths are recognized correctly on this OS.
func TestPathIsAbs(t *testing.T) {
	if !filepath.IsAbs("/data/x") {
		t.Fatal("/data/x should be absolute")
	}
}
