// Encorr: a media request app for Plex. Request the encore.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/FiTH-g1gGLe/encorr/internal/app"
	"github.com/FiTH-g1gGLe/encorr/internal/plex"
	"github.com/FiTH-g1gGLe/encorr/internal/store"
	"github.com/FiTH-g1gGLe/encorr/internal/tmdb"
	"github.com/FiTH-g1gGLe/encorr/internal/web"
)

func defaultConfigDir() string {
	if v := os.Getenv("ENCORR_CONFIG"); v != "" {
		return v
	}
	switch runtime.GOOS {
	case "windows":
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return filepath.Join(pd, "Encorr")
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "Encorr")
	default:
		if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
			return filepath.Join(x, "encorr")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "encorr")
	}
}

type options struct {
	port      int
	host      string
	configDir string
	noBrowser bool
	service   bool // running as the Windows background service
}

func main() {
	// Background-service commands come first: "encorr install" / "uninstall".
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "restart-service":
			_ = restartServiceHelper()
			return
		case "install", "uninstall":
			if err := serviceCommand(os.Args[1]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				pause()
				os.Exit(1)
			}
			return
		}
	}

	port := flag.Int("port", 5150, "port for the web interface")
	host := flag.String("host", "", "address to listen on (default: all)")
	configDir := flag.String("config", defaultConfigDir(), "folder for Encorr's data")
	noBrowser := flag.Bool("no-browser", false, "don't open the browser on start")
	asService := flag.Bool("service", false, "run as the Windows background service (set by 'encorr install')")
	version := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *version {
		fmt.Println("Encorr", app.Version)
		return
	}
	// Developer preview: point every outside service at the fake stack.
	if base := os.Getenv("ENCORR_DEV_FAKE"); base != "" {
		plex.PlexTVURL, plex.DiscoverURL, plex.AppAuthURL = base+"/plextv", base+"/discover", base+"/plexauth"
		tmdb.BaseURL = base + "/tmdb"
		app.SpeedTestURL(base + "/speedtest")
		app.UpdateAPIURL = base + "/github/release"
	}
	opts := options{port: *port, host: *host, configDir: *configDir, noBrowser: *noBrowser, service: *asService}
	// Remember where the program is now: an update renames the running file.
	exe, _ := os.Executable()
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	app.SetExePath(exe)
	cleanOldExe(exe)

	if *asService || isWindowsService() {
		opts.service, opts.noBrowser = true, true
		if err := runService(func(ctx context.Context) error {
			err := run(ctx, opts)
			if errors.Is(err, errRestart) {
				_ = spawnServiceRestart(exe)
			}
			return err
		}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	// Already running in the background? Then double-clicking the exe just
	// opens Encorr in the browser.
	local := fmt.Sprintf("http://localhost:%d", opts.port)
	if serviceRunning() {
		// A newer download? Update the background copy to it.
		if running := runningVersion(local); running != "" && running != app.Version && app.Version != "0.1.1" {
			fmt.Printf("Updating background Encorr from %s to %s (Windows will ask to allow changes)…\n", running, app.Version)
			if err := serviceCommand("install"); err != nil {
				fmt.Fprintln(os.Stderr, err)
				pause()
			}
			return
		}
		fmt.Println("Encorr is already running in the background. Opening", local)
		openBrowser(local)
		time.Sleep(1500 * time.Millisecond)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, opts); err != nil {
		if errors.Is(err, errRestart) {
			stop()
			args := os.Args[1:]
			if !slices.Contains(args, "-no-browser") && !slices.Contains(args, "--no-browser") {
				args = append(args, "-no-browser") // the open page reloads by itself
			}
			fmt.Println("Starting the new version…")
			_ = os.Setenv("ENCORR_RESTARTED", "1")
			if err := restartSelf(exe, args); err != nil {
				fmt.Fprintln(os.Stderr, "Couldn't start the new version:", err, "- start Encorr again by hand.")
				pause()
				os.Exit(1)
			}
			return
		}
		fmt.Fprintln(os.Stderr, err)
		pause()
		os.Exit(1)
	}
}

// errRestart means Encorr stopped to start its new version. As a Windows
// service, the non-zero exit makes Windows start it again (its recovery
// settings); otherwise main starts it.
var errRestart = errors.New("restarting for an update")

// cleanOldExe removes the copy an earlier update left behind.
func cleanOldExe(exe string) {
	if exe != "" {
		_ = os.Remove(exe + ".old")
		_ = os.Remove(exe + ".update")
	}
}

// run starts Encorr and blocks until ctx is cancelled.
func run(ctx context.Context, opts options) error {
	if err := os.MkdirAll(filepath.Join(opts.configDir, "logs"), 0o755); err != nil {
		return fmt.Errorf("Encorr can't create its data folder: %w", err)
	}
	logFile, err := os.OpenFile(filepath.Join(opts.configDir, "logs", "encorr.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	var out io.Writer = os.Stdout
	if opts.service {
		out = io.Discard // no console as a service
	}
	if err == nil {
		out = io.MultiWriter(out, logFile)
		app.LogPath = logFile.Name()
		defer logFile.Close()
	}
	logger := log.New(out, "", log.LstdFlags)

	st, err := store.Open(opts.configDir)
	if err != nil {
		logger.Printf("Encorr couldn't open its data: %v", err)
		return err
	}

	a := app.New(st, logger)
	a.Background = background{port: opts.port, service: opts.service}
	restart := make(chan struct{})
	var restartOnce sync.Once
	a.Restart = func() { restartOnce.Do(func() { close(restart) }) }
	addr := net.JoinHostPort(opts.host, fmt.Sprint(opts.port))
	// After an update the old copy may still be letting go of the port.
	ln, err := listenRetry(addr, opts.service || os.Getenv("ENCORR_RESTARTED") == "1")
	_ = os.Unsetenv("ENCORR_RESTARTED")
	if err != nil {
		logger.Printf("Port %d is already in use: is Encorr already running? (or start it with -port 5151)", opts.port)
		return fmt.Errorf("port %d is already in use", opts.port)
	}
	srv := &http.Server{
		Handler:           a.Handler(web.Static()),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	a.Start()

	local := fmt.Sprintf("http://localhost:%d", opts.port)
	if opts.service {
		logger.Printf("Encorr %s is running in the background. Open %s (data folder: %s)", app.Version, local, opts.configDir)
	} else {
		logger.Printf("Encorr %s is running. Open %s (data folder: %s)", app.Version, local, opts.configDir)
		if runtime.GOOS == "windows" {
			logger.Printf("Keep this window open while you use Encorr, or turn on \"Run in the background\" in Settings → General.")
		}
	}
	// Open the browser every time it's started by hand, so double-clicking
	// the exe always takes you to Encorr. (-no-browser for background use.)
	if !opts.noBrowser {
		go func() { time.Sleep(700 * time.Millisecond); openBrowser(local) }()
	}

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Printf("web server stopped: %v", err)
		}
	}()

	restarting := false
	select {
	case <-ctx.Done():
	case <-a.Quit(): // handing over to the background service
	case <-restart:
		restarting = true
	}
	logger.Printf("Stopping Encorr…")
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx)
	a.Stop()
	if restarting {
		logger.Printf("Restarting to finish the update…")
		return errRestart
	}
	return nil
}

// runningVersion asks the running Encorr which version it is.
func runningVersion(base string) string {
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(base + "/api/v1/status")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var st struct {
		Version string `json:"version"`
	}
	if json.NewDecoder(resp.Body).Decode(&st) != nil {
		return ""
	}
	return st.Version
}

// listenRetry waits a little for the port when starting as a service, in
// case the window version is still closing down.
func listenRetry(addr string, wait bool) (net.Listener, error) {
	deadline := time.Now().Add(20 * time.Second)
	for {
		ln, err := net.Listen("tcp", addr)
		if err == nil || !wait || time.Now().After(deadline) {
			return ln, err
		}
		time.Sleep(time.Second)
	}
}

// pause keeps the console window open on Windows so errors can be read.
func pause() {
	if runtime.GOOS == "windows" {
		fmt.Println("Press Enter to close.")
		_, _ = fmt.Scanln()
	}
}
