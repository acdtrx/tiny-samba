// Package smbconf renders a tiny-samba Config to a Samba smb.conf string.
// Output is deterministic (sorted share/user order) so it's golden-testable.
package smbconf

import (
	"fmt"
	"sort"
	"strings"

	"github.com/acdtrx/tiny-samba/internal/config"
)

const (
	// SMBUserName is the fixed Unix user that owns all share I/O.
	SMBUserName = "smbuser"
	// SMBGroupName is the matching Unix group.
	SMBGroupName = "smbgroup"
	// LogPath sends smbd's logs to stderr so they hit container stdout/stderr.
	LogPath = "/dev/stderr"
)

// Generate renders smb.conf from a validated Config.
func Generate(cfg *config.Config) string {
	var b strings.Builder

	writeGlobal(&b, cfg)

	names := sortedShareNames(cfg.Shares)
	for _, name := range names {
		writeShare(&b, name, cfg.Shares[name])
	}

	return b.String()
}

func writeGlobal(b *strings.Builder, cfg *config.Config) {
	b.WriteString("[global]\n")
	kv(b, "workgroup", cfg.Server.Workgroup)
	kv(b, "netbios name", cfg.Server.NetbiosName)
	kv(b, "server string", "tiny-samba")
	kv(b, "server min protocol", cfg.Server.MinProtocol)
	kv(b, "security", "user")
	kv(b, "passdb backend", "tdbsam")
	kv(b, "map to guest", "Bad User")
	kv(b, "log file", LogPath)
	// Default level 1, noisy subsystems silenced. quota:0 and vfs:0 mute
	// per-call container-quirk noise (overlayfs/bind-mount paths confuse
	// Samba's block-device lookup). sys_path_to_bdev() unfortunately logs
	// at DEBUG(0), which `log level` cannot suppress — that line stays.
	kv(b, "log level", "1 quota:0 vfs:0")
	kv(b, "max log size", "0")
	// We don't serve printers; turn the spooler off so smbd doesn't probe cupsd.
	kv(b, "load printers", "no")
	kv(b, "printing", "bsd")
	kv(b, "printcap name", "/dev/null")
	kv(b, "disable spoolss", "yes")
	kv(b, "show add printer wizard", "no")
	// We're not a domain member.
	kv(b, "dns proxy", "no")
	// We never run nmbd — explicitly disable any leftover NetBIOS code paths.
	kv(b, "disable netbios", "yes")
	// Skip user-defined shares (/var/lib/samba/usershares). We're the sole
	// share authority via smb.conf.
	kv(b, "usershare max shares", "0")
	// Hostname resolution: only DNS. Skip lmhosts / wins / broadcast probes
	// that would each take their own timeout.
	kv(b, "name resolve order", "host")
	// Don't host DFS shares — disabling stops smbd's DFS path parser from
	// kicking in on UNC requests and logging "parse_dfs_path_strict" errors
	// for ordinary share paths.
	kv(b, "host msdfs", "no")
	// Samba's internal idmap subsystem (separate from kernel idmapped mounts)
	// maps Windows SIDs ↔ Unix UIDs. We don't use it (no AD, no winbind),
	// but Samba logs a warning at startup if there's no fallback range
	// configured. tdb backend with a private high range is the canonical
	// "I don't actually use this" placeholder.
	kv(b, "idmap config * : backend", "tdb")
	kv(b, "idmap config * : range", "100000-200000")

	// Apple SMB extensions ("fruit" VFS module). Two big wins for macOS
	// Finder clients:
	//   1. Negotiates the server's "model" via AAPL extension so Finder
	//      shows a modern Time Capsule icon instead of the 90s monitor.
	//   2. Disables Spotlight RPCs (which Finder otherwise probes on every
	//      browse, blocks waiting for replies, and times out — major cause
	//      of "share list takes forever to load" symptoms).
	// Plus proper resource-fork / FinderInfo handling so .DS_Store and
	// extended attributes work without smearing across the share.
	// Harmless on Windows / Linux clients (negotiated only by Apple).
	// Skipped entirely if icon_model is set to "-" (escape hatch).
	if cfg.Server.IconModel != "-" {
		kv(b, "vfs objects", "catia fruit streams_xattr")
		kv(b, "fruit:metadata", "stream")
		kv(b, "fruit:model", cfg.Server.IconModel)
		kv(b, "fruit:posix_rename", "yes")
		kv(b, "fruit:veto_appledouble", "no")
		kv(b, "fruit:nfs_aces", "no")
		kv(b, "fruit:wipe_intentionally_left_blank_rfork", "yes")
		kv(b, "fruit:delete_empty_adouble", "yes")
		kv(b, "spotlight", "no")
	}
}

func writeShare(b *strings.Builder, name string, sh config.Share) {
	b.WriteString("\n")
	fmt.Fprintf(b, "[%s]\n", name)
	kv(b, "path", sh.Path)
	if sh.Comment != "" {
		kv(b, "comment", sh.Comment)
	}
	kv(b, "browseable", "yes")
	kv(b, "force user", SMBUserName)
	kv(b, "force group", SMBGroupName)
	kv(b, "create mask", "0660")
	kv(b, "directory mask", "0770")

	if sh.Guest {
		kv(b, "guest ok", "yes")
		kv(b, "guest only", "yes")
		kv(b, "read only", "yes")
		return
	}

	users, writeList := splitAccess(sh.Access)
	kv(b, "valid users", strings.Join(users, ", "))
	kv(b, "read only", "yes")
	if len(writeList) > 0 {
		kv(b, "write list", strings.Join(writeList, ", "))
	}
}

// kv writes one "    key = value" line.
func kv(b *strings.Builder, key, value string) {
	fmt.Fprintf(b, "    %s = %s\n", key, value)
}

func sortedShareNames(shares map[string]config.Share) []string {
	names := make([]string, 0, len(shares))
	for n := range shares {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// splitAccess returns (all-users-sorted, rw-users-sorted). The "ro" users are
// implied by being in the first list but not the second (smb.conf default for
// auth shares is read-only with write_list as the override).
func splitAccess(access map[string]string) (all, writers []string) {
	for u, level := range access {
		all = append(all, u)
		if level == "rw" {
			writers = append(writers, u)
		}
	}
	sort.Strings(all)
	sort.Strings(writers)
	return
}
