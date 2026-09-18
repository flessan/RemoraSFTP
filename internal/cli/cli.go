// Package cli implements the command-line interface. All commands operate the
// same engine services as the browser GUI - there is no second FTP/SFTP
// implementation. Running the binary with no arguments opens the interactive
// TUI launcher (the native front door); `start` launches the local server
// directly; one-shot commands (connect, ls, put, get, mkdir, rm, mv) spin up
// the engine in-process, perform the work using the shared
// connection/transfer managers, then exit.
package cli

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"

	"remorasftp/internal/app"
	"remorasftp/internal/browser"
	"remorasftp/internal/tui"
	"remorasftp/internal/version"
)

// Command is one CLI subcommand.
type Command struct {
	Name  string
	Short string
	Usage string
	Run   func(args []string, flags *Flags) error
}

// Flags is a minimal --flag value parser.
type Flags struct {
	values map[string]string
	bools  map[string]bool
}

func (f *Flags) String(name, def string) string {
	if v, ok := f.values[name]; ok {
		return v
	}
	return def
}

func (f *Flags) Int(name string, def int) int {
	if v, ok := f.values[name]; ok {
		n := 0
		for _, c := range v {
			if c < '0' || c > '9' {
				return def
			}
			n = n*10 + int(c-'0')
		}
		return n
	}
	return def
}

func (f *Flags) Bool(name string) bool { return f.bools[name] }

func parseFlags(args []string, bools map[string]bool, values map[string]bool) (positional []string, f *Flags) {
	fl := &Flags{values: map[string]string{}, bools: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--") {
			a = strings.TrimPrefix(a, "--")
			key := a
			val := ""
			inline := false
			if eq := strings.IndexByte(a, '='); eq >= 0 {
				key, val = a[:eq], a[eq+1:]
				inline = true
			}
			negate := false
			if strings.HasPrefix(key, "no-") {
				key = strings.TrimPrefix(key, "no-")
				negate = true
			}
			switch {
			case values[key]:
				if !inline && i+1 < len(args) {
					val = args[i+1]
					i++
				}
				fl.values[key] = val
			case bools[key]:
				fl.bools[key] = !negate
			default:
				fl.bools[key] = !negate
			}
		} else {
			positional = append(positional, a)
		}
	}
	return positional, fl
}

// commands returns the subcommand table.
func commands() []Command {
	return []Command{
		{
			Name:  "start",
			Short: "Start the local engine and serve the browser UI",
			Usage: "remorasftp start [--addr 127.0.0.1] [--port 0] [--no-browser] [--headless]",
			Run:   runStart,
		},
		{
			Name:  "version",
			Short: "Print version and build metadata",
			Usage: "remorasftp version",
			Run:   func(args []string, _ *Flags) error { return runVersion() },
		},
		{
			Name:  "status",
			Short: "Show whether a local engine is running",
			Usage: "remorasftp status",
			Run:   func(args []string, _ *Flags) error { return runStatus() },
		},
		{
			Name:  "profiles",
			Short: "List saved connection profiles",
			Usage: "remorasftp profiles",
			Run:   func(args []string, _ *Flags) error { return runProfiles() },
		},
		{
			Name:  "connect",
			Short: "Connect to a profile and print server info",
			Usage: "remorasftp connect <profile-name-or-id>",
			Run:   func(args []string, _ *Flags) error { return runConnect(args) },
		},
		{
			Name:  "ls",
			Short: "List a remote directory",
			Usage: "remorasftp ls <profile> [path]",
			Run:   func(args []string, _ *Flags) error { return runLs(args) },
		},
		{
			Name:  "put",
			Short: "Upload a local file to a remote path",
			Usage: "remorasftp put <profile> <local-path> <remote-path>",
			Run:   func(args []string, _ *Flags) error { return runPut(args) },
		},
		{
			Name:  "get",
			Short: "Download a remote file to a local path",
			Usage: "remorasftp get <profile> <remote-path> <local-path>",
			Run:   func(args []string, _ *Flags) error { return runGet(args) },
		},
		{
			Name:  "mkdir",
			Short: "Create a remote directory",
			Usage: "remorasftp mkdir <profile> <remote-path>",
			Run:   func(args []string, _ *Flags) error { return runMkdir(args) },
		},
		{
			Name:  "rm",
			Short: "Delete a remote file or directory",
			Usage: "remorasftp rm <profile> <remote-path> [--recursive]",
			Run:   runRm,
		},
		{
			Name:  "mv",
			Short: "Rename/move a remote file or directory",
			Usage: "remorasftp mv <profile> <from> <to>",
			Run:   func(args []string, _ *Flags) error { return runMv(args) },
		},
		{
			Name:  "help",
			Short: "Show help",
			Usage: "remorasftp help [command]",
			Run:   runHelp,
		},
	}
}

// Execute runs the CLI.
func Execute() error {
	if dir := envOrFlagDataDir(); dir != "" {
		os.Setenv("SFTPBOX_DATA_DIR", dir)
	}
	args := os.Args[1:]
	if len(args) == 0 {
		// No arguments: open the interactive TUI launcher when attached to a
		// terminal. In non-interactive contexts (piped stdin, CI, double-click
		// on Windows without a console) fall back to the classic behavior:
		// start the engine directly.
		if tuiWanted() {
			if err := tui.Run(os.Stdin, os.Stdout); err != nil {
				fmt.Fprintln(os.Stderr, "remorasftp: TUI unavailable ("+err.Error()+") - starting the engine directly.")
			} else {
				return nil
			}
		}
		return runStart(nil, &Flags{bools: map[string]bool{}, values: map[string]string{}})
	}
	cmdName := args[0]
	rest := args[1:]

	// Global --data-dir flag (any position before the subcommand is rare;
	// also handle after).
	var filtered []string
	for _, a := range rest {
		if strings.HasPrefix(a, "--data-dir=") {
			os.Setenv("SFTPBOX_DATA_DIR", strings.TrimPrefix(a, "--data-dir="))
			continue
		}
		filtered = append(filtered, a)
	}
	rest = filtered
	if strings.HasPrefix(cmdName, "--data-dir=") {
		os.Setenv("SFTPBOX_DATA_DIR", strings.TrimPrefix(cmdName, "--data-dir="))
		return runStart(nil, &Flags{bools: map[string]bool{}, values: map[string]string{}})
	}

	for _, c := range commands() {
		if c.Name == cmdName {
			boolFlags := map[string]bool{}
			valueFlags := map[string]bool{}
			if cmdName == "start" {
				valueFlags = map[string]bool{"addr": true, "port": true}
				boolFlags = map[string]bool{"no-browser": true, "headless": true, "allow-remote": true}
			}
			if cmdName == "rm" {
				boolFlags = map[string]bool{"recursive": true}
			}
			pos, fl := parseFlags(rest, boolFlags, valueFlags)
			return c.Run(pos, fl)
		}
	}
	if cmdName == "-h" || cmdName == "--help" || cmdName == "help" {
		return runHelp(rest, nil)
	}
	fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmdName)
	_ = runHelp(nil, nil)
	os.Exit(2)
	return nil
}

// tuiWanted reports whether the interactive TUI launcher should be used when
// the binary runs with no arguments. The launcher needs a real terminal on
// stdin; set REMORASFTP_NO_TUI=1 to force the classic direct-start behavior.
func tuiWanted() bool {
	if os.Getenv("REMORASFTP_NO_TUI") == "1" {
		return false
	}
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func envOrFlagDataDir() string {
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "--data-dir=") {
			return strings.TrimPrefix(a, "--data-dir=")
		}
	}
	return os.Getenv("SFTPBOX_DATA_DIR")
}

// isLoopbackAddr reports whether a bind host is a loopback address.
func isLoopbackAddr(addr string) bool {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func runHelp(args []string, _ *Flags) error {
	fmt.Println("RemoraSFTP - a local-first browser file manager for FTP, FTPS, and SFTP")
	fmt.Println()
	fmt.Println("The engine runs entirely on this device. The browser is only the UI.")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  remorasftp                interactive TUI launcher (control center)")
	fmt.Println("  remorasftp <command> [flags]")
	fmt.Println()
	fmt.Println("Commands:")
	for _, c := range commands() {
		fmt.Printf("  %-10s %s\n", c.Name, c.Short)
	}
	fmt.Println()
	fmt.Println("Global flags:")
	fmt.Println("  --data-dir=<path>   override the application data directory")
	fmt.Println()
	fmt.Println("Environment:")
	fmt.Println("  REMORASFTP_NO_TUI=1 run the classic engine directly instead of the TUI")
	fmt.Println()
	fmt.Printf("Version: %s (%s)\n", version.Version, version.Commit)
	return nil
}

func runStart(_ []string, fl *Flags) error {
	eng, err := app.New()
	if err != nil {
		return err
	}
	defer eng.Shutdown()

	addr := "127.0.0.1"
	port := 0
	noBrowser := false
	headless := false
	allowRemote := false
	if fl != nil {
		if a := fl.String("addr", ""); a != "" {
			addr = a
		}
		port = fl.Int("port", 0)
		noBrowser = fl.Bool("no-browser")
		headless = fl.Bool("headless")
		allowRemote = fl.Bool("allow-remote")
	}
	// Safety: refuse to bind beyond loopback unless explicitly opted in via
	// both the setting and the --allow-remote flag. A non-loopback bind
	// would expose the local API to the network.
	if !isLoopbackAddr(addr) && !allowRemote {
		return fmt.Errorf("refusing to listen on %q: the local API is loopback-only by default. Re-run with --allow-remote if you intentionally want to expose it to your network (see docs/security.md)", addr)
	}
	if err := eng.Start(addr, port); err != nil {
		return err
	}

	url := eng.BrowserURL()
	fmt.Println()
	fmt.Println("  RemoraSFTP is running.")
	fmt.Println()
	fmt.Printf("  Local interface : http://%s\n", eng.Addr())
	fmt.Printf("  Credential store: %s\n", eng.CredentialBackend())
	fmt.Printf("  Data directory  : %s\n", eng.DataDir())
	fmt.Println()
	fmt.Println("  Open the interface in your browser:")
	fmt.Printf("    %s\n", url)
	fmt.Println()
	fmt.Println("  Press Ctrl+C to stop.")
	fmt.Println()

	if !headless && !noBrowser {
		if err := browser.Open(url); err != nil {
			fmt.Fprintln(os.Stderr, "  (Could not launch a browser automatically; open the URL above.)")
		}
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	fmt.Println("\nShutting down…")
	return nil
}
