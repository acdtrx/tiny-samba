package cli

import (
	"errors"
	"flag"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/acdtrx/tiny-samba/internal/config"
)

// shutdownGrace is how long smbd has to clean up after we forward SIGTERM
// before we hard-kill it. Samba's clean shutdown is fast in practice;
// this is just an upper bound to keep `docker stop` from hanging.
const shutdownGrace = 15 * time.Second

// Serve is the container entrypoint. It bootstraps Samba's state from the
// declarative files, then supervises smbd (and optionally wsdd for Windows
// discoverability) until either child exits or the container receives a
// shutdown signal.
//
// We used to syscall.Exec smbd here so it became PID 1 directly. That model
// stopped working once we needed a second process (wsdd) sharing the same
// container lifecycle. Tini stays at PID 1 and reaps zombies; this process
// is the supervisor for the workload.
func Serve(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	configPath := fs.String("config", DefaultConfigPath, "path to config.yaml")
	passwordsPath := fs.String("passwords", DefaultPasswordsPath, "path to passwords.yaml")
	smbConfPath := fs.String("smb-conf", DefaultSmbConfPath, "path to write generated smb.conf")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	logf("starting up")
	logf("config:    %s", *configPath)
	logf("passwords: %s", *passwordsPath)

	cfg, pw, err := loadAndValidate(*configPath, *passwordsPath)
	if err != nil {
		logf("ERROR loading/validating config: %v", err)
		return 1
	}

	if missing := config.MissingPasswords(cfg, pw); len(missing) > 0 {
		for _, u := range missing {
			logf("WARN user %q declared but has no password — they cannot authenticate until passwords.yaml gets an entry and 'tiny-samba reload' is run", u)
		}
	}

	if err := applyConfig(cfg, pw, *smbConfPath); err != nil {
		logf("ERROR applying config: %v", err)
		return 1
	}

	logf("config applied; %d shares, %d users (%d with passwords)",
		len(cfg.Shares), len(cfg.Users), len(cfg.Users)-len(config.MissingPasswords(cfg, pw)))

	return supervise(cfg)
}

// supervise launches smbd (required) and wsdd (optional, when cfg.Server.WSD
// is enabled), forwards container signals to both, and returns smbd's exit
// code. wsdd is best-effort: if it fails to start or dies mid-run, we log
// and keep serving — Windows discoverability is a nicety, file serving is
// the actual job.
func supervise(cfg *config.Config) int {
	sigCh := make(chan os.Signal, 4)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	smbd, smbdDone, err := startSmbd()
	if err != nil {
		logf("ERROR start smbd: %v", err)
		return 1
	}
	logf("smbd started (pid %d)", smbd.Process.Pid)

	var wsddProc *os.Process
	var wsddDone <-chan error
	if cfg.Server.WSDEnabled() {
		wsdd, done, err := startWsdd(cfg.Server.Workgroup, cfg.Server.NetbiosName)
		if err != nil {
			logf("WARN start wsdd: %v — continuing without Windows Explorer discoverability", err)
		} else {
			wsddProc = wsdd.Process
			wsddDone = done
			logf("wsdd started (pid %d, workgroup=%s hostname=%s)",
				wsdd.Process.Pid, cfg.Server.Workgroup, cfg.Server.NetbiosName)
		}
	} else {
		logf("wsdd disabled by config (server.wsd: false) — Windows clients won't see this server in Explorer's Network sidebar")
	}

	shuttingDown := false

	for {
		select {
		case sig := <-sigCh:
			if shuttingDown {
				continue
			}
			shuttingDown = true
			logf("received %s, forwarding to children", sig)
			_ = smbd.Process.Signal(syscall.SIGTERM)
			if wsddProc != nil {
				_ = wsddProc.Signal(syscall.SIGTERM)
			}
			go func() {
				time.Sleep(shutdownGrace)
				if err := smbd.Process.Signal(syscall.Signal(0)); err == nil {
					logf("smbd still alive after %s, sending SIGKILL", shutdownGrace)
					_ = smbd.Process.Kill()
				}
			}()

		case err := <-smbdDone:
			if wsddProc != nil {
				_ = wsddProc.Signal(syscall.SIGTERM)
			}
			code := exitCode(err)
			if shuttingDown {
				logf("smbd exited (code %d) during shutdown", code)
			} else {
				logf("smbd exited unexpectedly (code %d): %v", code, err)
			}
			return code

		case err := <-wsddDone:
			wsddProc = nil
			wsddDone = nil
			if !shuttingDown {
				logf("WARN wsdd exited: %v — Windows discoverability is now offline; smbd continues serving", err)
			}
		}
	}
}

func startSmbd() (*exec.Cmd, <-chan error, error) {
	path, err := exec.LookPath("smbd")
	if err != nil {
		return nil, nil, err
	}
	cmd := exec.Command(path, "--foreground", "--no-process-group", "--debug-stdout")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	return cmd, done, nil
}

func startWsdd(workgroup, hostname string) (*exec.Cmd, <-chan error, error) {
	path, err := exec.LookPath("wsdd2")
	if err != nil {
		return nil, nil, err
	}
	// wsdd2 (C) flag set differs from the Python wsdd:
	//   -G workgroup   - SMB workgroup
	//   -N netbios     - NetBIOS name (advertised via LLMNR, must match SMB)
	//   -H hostname    - host name (advertised via WSD)
	// We use the same string for -N and -H since tiny-samba uses one identity
	// across both protocols. wsdd2 runs in foreground by default (use -d to
	// daemonize) and logs to stderr — exactly what we want under our supervisor.
	cmd := exec.Command(path, "-G", workgroup, "-N", hostname, "-H", hostname)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	return cmd, done, nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 1
}
