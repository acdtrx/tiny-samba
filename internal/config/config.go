// Package config defines the on-disk YAML schema for tiny-samba and provides
// loading + validation. Two files: config.yaml (declarative structure) and
// passwords.yaml (secret material). Both are RO-mounted at runtime.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the structural state — shares, declared user names, server settings.
// Read from config.yaml.
type Config struct {
	Server Server           `yaml:"server"`
	Users  []string         `yaml:"users"`
	Shares map[string]Share `yaml:"shares"`
}

// Server holds global Samba settings.
type Server struct {
	Workgroup   string `yaml:"workgroup,omitempty"`
	NetbiosName string `yaml:"netbios_name,omitempty"`
	DataUID     int    `yaml:"data_uid,omitempty"`
	MinProtocol string `yaml:"min_protocol,omitempty"`
	// IconModel is sent to macOS clients via Samba's `fruit` VFS module to
	// pick a Finder sidebar icon. Apple model strings macOS recognizes
	// include: TimeCapsule6,106 (white box, NAS-like), Xserve1,1 (server),
	// MacPro6,1 (cylinder Mac Pro), Macmini7,1, etc. Set to empty string to
	// disable the override (Finder falls back to its old monitor icon).
	IconModel string `yaml:"icon_model,omitempty"`
	// WSD enables a wsdd sidecar process that responds to Windows
	// WS-Discovery probes so the server appears in Explorer's Network
	// sidebar. Pointer so an omitted YAML field defaults to true (a plain
	// bool can't distinguish "absent" from "explicit false"). Set
	// `server.wsd: false` to suppress the wsdd process entirely.
	WSD *bool `yaml:"wsd,omitempty"`
}

// WSDEnabled returns the resolved boolean state of the WSD field.
// Always call ApplyDefaults first, which guarantees WSD is non-nil.
func (s *Server) WSDEnabled() bool {
	return s.WSD != nil && *s.WSD
}

// Share defines one SMB share.
type Share struct {
	Path    string            `yaml:"path"`
	Comment string            `yaml:"comment,omitempty"`
	Guest   bool              `yaml:"guest,omitempty"`
	Access  map[string]string `yaml:"access,omitempty"`
}

// Passwords maps username → password material. The value is either plaintext
// (default) or "$NT$<32-hex>" for a pre-computed NT hash.
type Passwords map[string]string

const (
	DefaultWorkgroup   = "WORKGROUP"
	DefaultNetbiosName = "tiny-samba"
	DefaultDataUID     = 1000
	DefaultMinProto    = "SMB3"
	// DefaultIconModel makes macOS Finder show the modern "Time Capsule"
	// (white box) icon instead of the early-2000s CRT monitor icon.
	DefaultIconModel = "TimeCapsule6,106"
)

// ApplyDefaults fills zero-valued server fields with their defaults.
// Note: IconModel is NOT defaulted here because empty is a valid choice
// (means "don't enable fruit:model"). Use the explicit DefaultIconModel
// constant when generating from a fresh config.
func (s *Server) ApplyDefaults() {
	if s.Workgroup == "" {
		s.Workgroup = DefaultWorkgroup
	}
	if s.NetbiosName == "" {
		s.NetbiosName = DefaultNetbiosName
	}
	if s.DataUID == 0 {
		s.DataUID = DefaultDataUID
	}
	if s.MinProtocol == "" {
		s.MinProtocol = DefaultMinProto
	}
	// IconModel: yaml unmarshals missing fields to "". Treat "" as "use the
	// sane default" since the icon override is overwhelmingly desirable.
	// Users who explicitly want no override can set `icon_model: "-"` in
	// the YAML and we'll suppress it (handled in smbconf generation).
	if s.IconModel == "" {
		s.IconModel = DefaultIconModel
	}
	if s.WSD == nil {
		t := true
		s.WSD = &t
	}
}

// LoadConfig reads and parses config.yaml.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg.Server.ApplyDefaults()
	cfg.normalize()
	return &cfg, nil
}

// LoadPasswords reads and parses passwords.yaml. A missing file yields an
// empty Passwords (callers should treat this as "all users have no password set"
// and warn accordingly).
func LoadPasswords(path string) (Passwords, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Passwords{}, nil
		}
		return nil, fmt.Errorf("read passwords %s: %w", path, err)
	}
	var pw Passwords
	if err := yaml.Unmarshal(data, &pw); err != nil {
		return nil, fmt.Errorf("parse passwords %s: %w", path, err)
	}
	if pw == nil {
		pw = Passwords{}
	}
	return pw, nil
}

func (c *Config) normalize() {
	for name, sh := range c.Shares {
		for u, level := range sh.Access {
			sh.Access[u] = strings.ToLower(level)
		}
		c.Shares[name] = sh
	}
}

var (
	nameRe    = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,30}$`)
	netbiosRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,15}$`)
	shareRe   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

var (
	validProtos = map[string]bool{"SMB1": true, "SMB2": true, "SMB3": true}
	validAccess = map[string]bool{"ro": true, "rw": true}
)

// Validate checks structural correctness and cross-references between the two
// files. Returns the first error encountered. Callers should call this before
// applying the config.
func Validate(cfg *Config, pw Passwords) error {
	if !netbiosRe.MatchString(cfg.Server.Workgroup) {
		return fmt.Errorf("server.workgroup %q invalid (1-15 alphanumeric/_/-)", cfg.Server.Workgroup)
	}
	if !netbiosRe.MatchString(cfg.Server.NetbiosName) {
		return fmt.Errorf("server.netbios_name %q invalid (1-15 alphanumeric/_/-)", cfg.Server.NetbiosName)
	}
	if cfg.Server.DataUID < 100 || cfg.Server.DataUID > 65000 {
		return fmt.Errorf("server.data_uid %d out of range (100-65000)", cfg.Server.DataUID)
	}
	if !validProtos[cfg.Server.MinProtocol] {
		return fmt.Errorf("server.min_protocol %q invalid (SMB1/SMB2/SMB3)", cfg.Server.MinProtocol)
	}

	declared := make(map[string]bool, len(cfg.Users))
	for _, u := range cfg.Users {
		if !nameRe.MatchString(u) {
			return fmt.Errorf("user %q invalid (lowercase letter start, then [a-z0-9_-], 1-31 chars)", u)
		}
		if declared[u] {
			return fmt.Errorf("user %q declared twice", u)
		}
		declared[u] = true
	}

	if len(cfg.Shares) == 0 {
		return fmt.Errorf("no shares declared")
	}
	for name, sh := range cfg.Shares {
		if !shareRe.MatchString(name) {
			return fmt.Errorf("share name %q invalid (1-64 alphanumeric/_/-)", name)
		}
		if sh.Path == "" || !filepath.IsAbs(sh.Path) {
			return fmt.Errorf("share %q: path must be an absolute path", name)
		}
		if sh.Guest && len(sh.Access) > 0 {
			return fmt.Errorf("share %q: cannot have both guest=true and access list", name)
		}
		if !sh.Guest && len(sh.Access) == 0 {
			return fmt.Errorf("share %q: must have either guest=true or access list", name)
		}
		for u, level := range sh.Access {
			if !declared[u] {
				return fmt.Errorf("share %q: user %q not declared in users", name, u)
			}
			if !validAccess[level] {
				return fmt.Errorf("share %q user %q: access level %q invalid (ro|rw)", name, u, level)
			}
		}
	}

	for u := range pw {
		if !declared[u] {
			return fmt.Errorf("password set for non-declared user %q (declare in config.yaml first)", u)
		}
	}

	return nil
}

// MissingPasswords returns declared users with no entry in pw.
// Used at startup to warn the operator.
func MissingPasswords(cfg *Config, pw Passwords) []string {
	var missing []string
	for _, u := range cfg.Users {
		if _, ok := pw[u]; !ok {
			missing = append(missing, u)
		}
	}
	return missing
}
