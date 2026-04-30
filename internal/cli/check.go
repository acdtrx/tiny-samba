package cli

import (
	"flag"
	"fmt"

	"github.com/acdtrx/tiny-samba/internal/config"
)

// Check validates config + passwords without applying anything. Exit 0 = valid,
// 1 = invalid. Useful before restarting or in CI.
func Check(args []string) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	configPath := fs.String("config", DefaultConfigPath, "path to config.yaml")
	passwordsPath := fs.String("passwords", DefaultPasswordsPath, "path to passwords.yaml")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, pw, err := loadAndValidate(*configPath, *passwordsPath)
	if err != nil {
		fmt.Println("INVALID:", err)
		return 1
	}

	fmt.Printf("OK: %d shares, %d users\n", len(cfg.Shares), len(cfg.Users))
	if missing := config.MissingPasswords(cfg, pw); len(missing) > 0 {
		fmt.Println("WARN: users without passwords:")
		for _, u := range missing {
			fmt.Println("  -", u)
		}
	}
	return 0
}
