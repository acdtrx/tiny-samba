package cli

import (
	"flag"
	"fmt"
	"sort"
)

// List prints declared users, shares, and per-user share access.
// Read-only inspection; never touches Samba state.
func List(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	configPath := fs.String("config", DefaultConfigPath, "path to config.yaml")
	passwordsPath := fs.String("passwords", DefaultPasswordsPath, "path to passwords.yaml")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, pw, err := loadAndValidate(*configPath, *passwordsPath)
	if err != nil {
		fmt.Println("ERROR:", err)
		return 1
	}

	wsd := "off"
	if cfg.Server.WSDEnabled() {
		wsd = "on"
	}
	fmt.Printf("Server: workgroup=%s netbios=%s data_uid=%d min_protocol=%s wsd=%s\n",
		cfg.Server.Workgroup, cfg.Server.NetbiosName, cfg.Server.DataUID, cfg.Server.MinProtocol, wsd)

	fmt.Println("\nUsers:")
	users := append([]string(nil), cfg.Users...)
	sort.Strings(users)
	for _, u := range users {
		marker := "  "
		if _, ok := pw[u]; !ok {
			marker = "! "
		}
		fmt.Printf("  %s%s\n", marker, u)
	}
	fmt.Println("  (! = no password set)")

	fmt.Println("\nShares:")
	shareNames := make([]string, 0, len(cfg.Shares))
	for n := range cfg.Shares {
		shareNames = append(shareNames, n)
	}
	sort.Strings(shareNames)
	for _, name := range shareNames {
		sh := cfg.Shares[name]
		if sh.Guest {
			fmt.Printf("  %s  [guest, ro]  %s\n", name, sh.Path)
			continue
		}
		fmt.Printf("  %s  %s\n", name, sh.Path)
		accessNames := make([]string, 0, len(sh.Access))
		for u := range sh.Access {
			accessNames = append(accessNames, u)
		}
		sort.Strings(accessNames)
		for _, u := range accessNames {
			fmt.Printf("      %s: %s\n", u, sh.Access[u])
		}
	}

	return 0
}
