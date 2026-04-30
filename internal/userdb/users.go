package userdb

import (
	"fmt"
	"os/exec"
	"os/user"
	"strconv"
)

// EnsureGroup creates the named group with the given gid if it doesn't exist.
// Idempotent: returns nil if a group with that name already exists, regardless
// of whether its gid matches.
func EnsureGroup(name string, gid int) error {
	if _, err := user.LookupGroup(name); err == nil {
		return nil
	}
	out, err := exec.Command("groupadd", "-g", strconv.Itoa(gid), name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("groupadd %s: %w (output: %s)", name, err, string(out))
	}
	return nil
}

// EnsureSystemUser creates a service-style user (no home, no shell, no per-user
// group) attached to the named group. uid > 0 forces a specific UID; uid == 0
// auto-assigns. Idempotent on user name (does not check uid).
func EnsureSystemUser(name string, uid int, group string) error {
	if _, err := user.Lookup(name); err == nil {
		return nil
	}
	args := []string{"-M", "-s", "/sbin/nologin", "-N", "-g", group}
	if uid > 0 {
		args = append(args, "-u", strconv.Itoa(uid))
	}
	args = append(args, name)
	out, err := exec.Command("useradd", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("useradd %s: %w (output: %s)", name, err, string(out))
	}
	return nil
}

// DeleteUser removes a Unix user if present. Best-effort.
func DeleteUser(name string) error {
	if _, err := user.Lookup(name); err != nil {
		return nil
	}
	out, err := exec.Command("userdel", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("userdel %s: %w (output: %s)", name, err, string(out))
	}
	return nil
}

// UserExists is a small wrapper around user.Lookup that hides the error type.
func UserExists(name string) bool {
	_, err := user.Lookup(name)
	return err == nil
}
