package cli

import (
	"flag"

	"github.com/acdtrx/tiny-samba/internal/userdb"
)

// Reload re-reads both YAML files, regenerates Unix users + passdb + smb.conf,
// then asks smbd to reload its config without restarting. Active SMB sessions
// are unaffected; new auths use the new passwords/permissions.
func Reload(args []string) int {
	fs := flag.NewFlagSet("reload", flag.ContinueOnError)
	configPath := fs.String("config", DefaultConfigPath, "path to config.yaml")
	passwordsPath := fs.String("passwords", DefaultPasswordsPath, "path to passwords.yaml")
	smbConfPath := fs.String("smb-conf", DefaultSmbConfPath, "path to write generated smb.conf")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, pw, err := loadAndValidate(*configPath, *passwordsPath)
	if err != nil {
		logf("ERROR loading/validating config: %v", err)
		return 1
	}

	if err := applyConfig(cfg, pw, *smbConfPath); err != nil {
		logf("ERROR applying config: %v", err)
		return 1
	}

	if err := userdb.ReloadSmbd(); err != nil {
		logf("ERROR signaling smbd: %v", err)
		logf("smb.conf and passdb updated, but smbd is still using old config — restart the container to apply")
		return 1
	}

	logf("reloaded: %d shares, %d users", len(cfg.Shares), len(cfg.Users))
	return 0
}
