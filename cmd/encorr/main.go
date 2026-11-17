// Encorr: a media request app for Plex. Request the encore.
package main

import (
	"context"
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

func main() {
	port := flag.Int("port", 5150, "port for the web interface")
	host := flag.String("host", "", "address to listen on (default: all)")
	configDir := flag.String("config", defaultConfigDir(), "folder for Encorr's data")
	noBrowser := flag.Bool("no-browser", false, "don't open the browser on start")
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
	}

	if err := os.MkdirAll(filepath.Join(*configDir, "logs"), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "Encorr can't create its data folder:", err)
		pause()
		os.Exit(1)
	}
	logFile, err := os.OpenFile(filepath.Join(*configDir, "logs", "encorr.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	var out io.Writer = os.Stdout
	if err == nil {
		out = io.MultiWriter(os.Stdout, logFile)
		defer logFile.Close()
	}
	logger := log.New(out, "", log.LstdFlags)

	st, err := store.Open(*configDir)
	if err != nil {
		logger.Printf("Encorr couldn't open its data: %v", err)
		pause()
		os.Exit(1)
	}

	a := app.New(st, logger)
	addr := net.JoinHostPort(*host, fmt.Sprint(*port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Printf("Port %d is already in use: is Encorr already running? (or start it with -port 5151)", *port)
		pause()
		os.Exit(1)
	}
	srv := &http.Server{
		Handler:           a.Handler(web.Static()),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	a.Start()

	local := fmt.Sprintf("http://localhost:%d", *port)
	logger.Printf("Encorr %s is running. Open %s (data folder: %s)", app.Version, local, *configDir)
	logger.Printf("Press Ctrl+C to stop.")
	setupDone := false
	st.Read(func(d *store.Data) { setupDone = d.Settings.SetupComplete })
	if !*noBrowser && !setupDone {
		go func() { time.Sleep(700 * time.Millisecond); openBrowser(local) }()
	}

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Printf("web server stopped: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	logger.Printf("Stopping Encorr…")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	a.Stop()
}

// pause keeps the console window open on Windows so errors can be read.
func pause() {
	if runtime.GOOS == "windows" {
		fmt.Println("Press Enter to close.")
		_, _ = fmt.Scanln()
	}
}
