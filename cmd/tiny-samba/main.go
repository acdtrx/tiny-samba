// Command tiny-samba is the entrypoint for the tiny-samba container and CLI.
//
// Subcommands:
//   serve   - container entrypoint (load config + apply + supervise smbd/wsdd)
//   reload  - re-read config and signal smbd without restart
//   check   - validate config without applying
//   list    - print declared users + shares
package main

import (
	"fmt"
	"os"

	"github.com/acdtrx/tiny-samba/internal/cli"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	sub := os.Args[1]
	args := os.Args[2:]

	switch sub {
	case "serve":
		os.Exit(cli.Serve(args))
	case "reload":
		os.Exit(cli.Reload(args))
	case "check":
		os.Exit(cli.Check(args))
	case "list":
		os.Exit(cli.List(args))
	case "version", "--version", "-v":
		fmt.Println("tiny-samba", version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %q\n\n", sub)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `tiny-samba %s — minimal Samba container wrapper

Usage:
  tiny-samba <subcommand> [flags]

Subcommands:
  serve     Container entrypoint: bootstrap state and supervise smbd
            (and wsdd for Windows discoverability, when enabled).
  reload    Re-read config + passwords, regenerate state, signal smbd
            (no restart, no dropped sessions).
  check     Validate config + passwords without applying anything.
            Exit 0 if valid, 1 if not.
  list      Print declared users and share access (read-only).
  version   Print version and exit.
  help      This message.

Common flags (most subcommands):
  -config <path>      config.yaml location    (default %s)
  -passwords <path>   passwords.yaml location (default %s)
  -smb-conf <path>    smb.conf write location (default %s)

Examples:
  tiny-samba serve
  tiny-samba check -config ./examples/config.yaml -passwords ./examples/passwords.yaml
  docker exec tiny-samba tiny-samba reload
`, version, cli.DefaultConfigPath, cli.DefaultPasswordsPath, cli.DefaultSmbConfPath)
}
