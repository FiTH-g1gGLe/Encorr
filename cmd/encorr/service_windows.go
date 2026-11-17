package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/FiTH-g1gGLe/encorr/internal/app"
)

const svcName = "Encorr"

func isWindowsService() bool {
	ok, err := svc.IsWindowsService()
	return err == nil && ok
}

// runService hands control to Windows and runs Encorr until Windows says stop.
func runService(run func(ctx context.Context) error) error {
	return svc.Run(svcName, &handler{run: run})
}

type handler struct{ run func(ctx context.Context) error }

func (h *handler) Execute(_ []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.run(ctx) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case err := <-done:
			cancel()
			if err != nil {
				return true, 1 // non-zero: Windows' recovery restarts us
			}
			return false, 0
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				select {
				case <-done:
				case <-time.After(15 * time.Second):
				}
				return false, 0
			}
		}
	}
}

// queryService reports whether the Encorr service exists and is running,
// using only the rights a normal (non-admin) user has.
func queryService() (installed, running bool) {
	m, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return false, false
	}
	defer windows.CloseServiceHandle(m)
	name, _ := windows.UTF16PtrFromString(svcName)
	h, err := windows.OpenService(m, name, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return false, false
	}
	defer windows.CloseServiceHandle(h)
	var st windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(h, &st); err != nil {
		return true, false
	}
	return true, st.CurrentState == windows.SERVICE_RUNNING || st.CurrentState == windows.SERVICE_START_PENDING
}

func serviceRunning() bool { _, r := queryService(); return r }

func isAdmin() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// runElevated starts this exe again with admin rights (Windows shows its
// "allow changes?" prompt) and the given arguments.
func runElevated(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(exe)
	params, _ := syscall.UTF16PtrFromString(strings.Join(args, " "))
	dir, _ := syscall.UTF16PtrFromString(filepath.Dir(exe))
	shell32 := syscall.NewLazyDLL("shell32.dll")
	r, _, _ := shell32.NewProc("ShellExecuteW").Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(params)), uintptr(unsafe.Pointer(dir)), 1)
	if r <= 32 {
		return errors.New("Windows didn't allow it (the 'allow changes' prompt was cancelled)")
	}
	return nil
}

// installDir is where the background copy of Encorr lives, so moving or
// deleting the downloaded file doesn't break it.
func installDir() string {
	pf := os.Getenv("ProgramFiles")
	if pf == "" {
		pf = `C:\Program Files`
	}
	return filepath.Join(pf, "Encorr")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	_ = os.Remove(dst + ".old")
	if _, err := os.Stat(dst); err == nil {
		// A running exe can be renamed but not overwritten.
		if err := os.Rename(dst, dst+".old"); err != nil {
			return err
		}
	}
	return os.Rename(tmp, dst)
}

// serviceCommand handles "encorr install" and "encorr uninstall". They need
// admin rights; without them Encorr asks Windows for them.
func serviceCommand(cmd string) error {
	if !isAdmin() {
		return runElevated(cmd)
	}
	fmt.Printf("Encorr %s: %s background service…\n", app.Version, map[string]string{"install": "setting up the", "uninstall": "removing the"}[cmd])
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("couldn't open Windows services: %w", err)
	}
	defer m.Disconnect()

	// Remove any existing copy first (also how "install" updates it).
	if s, err := m.OpenService(svcName); err == nil {
		_, _ = s.Control(svc.Stop)
		waitStopped(s)
		_ = s.Delete()
		s.Close()
		time.Sleep(2 * time.Second) // Windows finishes deleting
	}
	if cmd == "uninstall" {
		fmt.Println("Done: Encorr no longer runs in the background.")
		return nil
	}

	src, err := os.Executable()
	if err != nil {
		return err
	}
	dir := installDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	dst := filepath.Join(dir, "encorr.exe")
	if !strings.EqualFold(filepath.Clean(src), filepath.Clean(dst)) {
		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("couldn't copy Encorr to %s: %w", dir, err)
		}
	}

	s, err := m.CreateService(svcName, dst, mgr.Config{
		DisplayName:      "Encorr",
		Description:      "Encorr media requests for Plex (http://localhost:5150). Request the encore.",
		StartType:        mgr.StartAutomatic,
		DelayedAutoStart: true, // after Plex, Radarr and Sonarr
	}, "-service")
	if err != nil {
		return fmt.Errorf("couldn't create the service: %w", err)
	}
	defer s.Close()
	// If it ever stops unexpectedly, Windows starts it again.
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}, 24*60*60)
	_ = s.SetRecoveryActionsOnNonCrashFailures(true)

	if err := s.Start(); err != nil {
		return fmt.Errorf("installed, but it didn't start: %w", err)
	}
	fmt.Println("Done: Encorr now runs in the background and starts with Windows.")
	go openBrowser("http://localhost:5150")
	time.Sleep(2 * time.Second)
	return nil
}

func waitStopped(s *mgr.Service) {
	for i := 0; i < 30; i++ {
		st, err := s.Query()
		if err != nil || st.State == svc.Stopped {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// background implements app.Background for the web Settings page.
type background struct {
	port    int
	service bool
}

func (b background) Status() app.BackgroundStatus {
	inst, run := queryService()
	return app.BackgroundStatus{Supported: true, Installed: inst, Running: run, IsService: b.service}
}

func (b background) Install(quit func()) error {
	if b.service {
		return nil // already the background copy
	}
	if err := runElevated("install"); err != nil {
		return err
	}
	// Close this window copy once the service exists, so it can take the port.
	go func() {
		for i := 0; i < 240; i++ {
			if inst, _ := queryService(); inst {
				time.Sleep(500 * time.Millisecond)
				quit()
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()
	return nil
}

func (b background) Remove(quit func()) error {
	if !b.service {
		return runElevated("uninstall")
	}
	// The background copy runs with full rights: it can remove itself.
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(svcName)
	if err != nil {
		return err
	}
	defer s.Close()
	if err := s.Delete(); err != nil {
		return err
	}
	go func() { time.Sleep(time.Second); quit() }()
	return nil
}
