package userdb

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"time"
)

// SyncPassdb wipes Samba's passdb and rebuilds it from the given password
// entries. Each entry is hashed (or accepted as a pre-hashed $NT$ value) and
// inserted via a temporary smbpasswd-format file imported with `pdbedit -i`.
//
// Callers must have already created Unix accounts for every user in entries
// (passdb requires a matching uid).
func SyncPassdb(entries map[string]string) error {
	if err := wipePassdb(); err != nil {
		return fmt.Errorf("wipe passdb: %w", err)
	}
	if len(entries) == 0 {
		return nil
	}

	tmp, err := os.CreateTemp("", "tiny-samba-*.smbpasswd")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmp.Name())

	now := time.Now().Unix()
	for name, raw := range entries {
		hash, err := NormalizeHash(raw)
		if err != nil {
			tmp.Close()
			return fmt.Errorf("user %s: %w", name, err)
		}
		u, err := user.Lookup(name)
		if err != nil {
			tmp.Close()
			return fmt.Errorf("user %s: lookup uid: %w", name, err)
		}
		// smbpasswd line: name:uid:LM:NT:[flags]:LCT-<hex>:
		// LM hash disabled (32 X's). Flags: U=normal user, padded to 11 chars.
		_, err = fmt.Fprintf(tmp, "%s:%s:%s:%s:[U          ]:LCT-%X:\n",
			name, u.Uid, strings.Repeat("X", 32), hash, now)
		if err != nil {
			tmp.Close()
			return fmt.Errorf("write temp: %w", err)
		}
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}

	out, err := exec.Command("pdbedit", "-i", "smbpasswd:"+tmp.Name()).CombinedOutput()
	if err != nil {
		return fmt.Errorf("pdbedit import: %w (output: %s)", err, string(out))
	}
	return nil
}

// wipePassdb removes every entry from the active tdbsam. Best-effort: returns
// nil even if pdbedit -L fails (an empty / nonexistent passdb is fine).
func wipePassdb() error {
	out, err := exec.Command("pdbedit", "-L").Output()
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		name, _, _ := strings.Cut(line, ":")
		if name == "" {
			continue
		}
		_ = exec.Command("pdbedit", "-x", "-u", name).Run()
	}
	return nil
}

// ReloadSmbd asks a running smbd to re-read smb.conf without restarting.
// Active sessions are unaffected. Safe to call when smbd isn't running
// (returns the underlying error which the caller can choose to ignore).
func ReloadSmbd() error {
	out, err := exec.Command("smbcontrol", "all", "reload-config").CombinedOutput()
	if err != nil {
		return fmt.Errorf("smbcontrol reload-config: %w (output: %s)", err, string(out))
	}
	return nil
}
