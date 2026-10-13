// Package cli implements the tiny-samba subcommands. Each subcommand has its
// own file and its own flag set.
package cli

import (
	"fmt"
	"os"

	"github.com/acdtrx/tiny-samba/internal/config"
	"github.com/acdtrx/tiny-samba/internal/smbconf"
	"github.com/acdtrx/tiny-samba/internal/userdb"
)

// Default on-disk locations inside the container.
const (
	DefaultConfigPath    = "/etc/tiny-samba/config.yaml"
	DefaultPasswordsPath = "/etc/tiny-samba/passwords.yaml"
	DefaultSmbConfPath   = "/etc/samba/smb.conf"
)

// LogPrefix is prepended to every line tiny-samba prints. Lets users grep for
// our messages distinctly from smbd's interleaved output.
const LogPrefix = "[tiny-samba] "

// logf writes a line to stderr with the tiny-samba prefix.
func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, LogPrefix+format+"\n", args...)
}

// loadAndValidate is shared startup boilerplate: read both files, apply
// defaults, validate. On success returns config + passwords; on failure
// returns the error verbatim (no logging — caller decides what to do with it).
func loadAndValidate(configPath, passwordsPath string) (*config.Config, config.Passwords, error) {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, nil, err
	}
	pw, err := config.LoadPasswords(passwordsPath)
	if err != nil {
		return nil, nil, err
	}
	if err := config.Validate(cfg, pw); err != nil {
		return nil, nil, err
	}
	return cfg, pw, nil
}

// applyConfig is the shared "make the running system match the files" routine
// used by both serve (one-shot at startup) and reload (mid-flight). It:
//  1. Ensures Samba's runtime directories exist (bind mounts wipe out
//     anything the image created underneath them).
//  2. Ensures Unix accounts exist (smbgroup, smbuser, every declared user).
//  3. Writes a fresh smb.conf to smbConfPath.
//  4. Wipes + rebuilds Samba's passdb from passwords.
//
// It does NOT signal smbd — the caller decides (serve execs smbd after; reload
// calls smbcontrol).
func applyConfig(cfg *config.Config, pw config.Passwords, smbConfPath string) error {
	// Re-create directories smbd / pdbedit need. Cases this handles:
	//   - /var/lib/samba is bind-mounted (orchestrator's mount source has no
	//     'private' subdir, so the image's mkdir doesn't survive).
	//   - /run/samba lives on the OCI default tmpfs at /run, which is empty
	//     on every container start.
	//   - /etc/samba may or may not exist; we own the smb.conf inside it.
	for _, d := range []struct {
		path string
		mode os.FileMode
	}{
		{"/etc/samba", 0755},
		{"/var/lib/samba/private", 0700},
		{"/var/cache/samba", 0755},
		{"/var/log/samba", 0755},
		{"/run/samba", 0755},
	} {
		if err := os.MkdirAll(d.path, d.mode); err != nil {
			return fmt.Errorf("mkdir %s: %w", d.path, err)
		}
	}

	if err := userdb.EnsureGroup(smbconf.SMBGroupName, cfg.Server.DataUID); err != nil {
		return err
	}
	if err := userdb.EnsureSystemUser(smbconf.SMBUserName, cfg.Server.DataUID, smbconf.SMBGroupName); err != nil {
		return err
	}
	for _, u := range cfg.Users {
		if err := userdb.EnsureSystemUser(u, 0, smbconf.SMBGroupName); err != nil {
			return err
		}
	}

	// Write smb.conf BEFORE touching the passdb. pdbedit reads smb.conf to
	// find the passdb backend; if our config isn't there yet, it'd use
	// Debian's default smb.conf (or fail), which can pick a different
	// backend than we want.
	conf := smbconf.Generate(cfg, probeNoXattrShares(cfg))
	if err := os.WriteFile(smbConfPath, []byte(conf), 0644); err != nil {
		return err
	}

	// Only sync passdb for users that actually have a password set.
	withPasswords := make(map[string]string, len(pw))
	for _, u := range cfg.Users {
		if entry, ok := pw[u]; ok {
			withPasswords[u] = entry
		}
	}
	if err := userdb.SyncPassdb(withPasswords); err != nil {
		return err
	}

	return nil
}

// probeNoXattrShares returns the set of share names whose path can't store
// extended attributes (exFAT, vfat, some CIFS mounts). Probed on every apply
// because a share's backing filesystem can change between reloads.
func probeNoXattrShares(cfg *config.Config) map[string]bool {
	noXattr := map[string]bool{}
	for name, sh := range cfg.Shares {
		ok, err := supportsXattr(sh.Path)
		if err != nil {
			logf("WARN share %q: could not probe extended attribute support at %s: %v — keeping default config", name, sh.Path, err)
			continue
		}
		if !ok {
			noXattr[name] = true
			logf("WARN share %q: filesystem at %s has no extended attribute support — Apple extensions, alternate data streams and stored DOS attributes are off for this share", name, sh.Path)
		}
	}
	return noXattr
}
