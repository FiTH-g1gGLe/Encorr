//go:build !windows

package main

import (
	"context"
	"errors"

	"github.com/FiTH-g1gGLe/encorr/internal/app"
)

func isWindowsService() bool { return false }
func serviceRunning() bool   { return false }

func runService(run func(ctx context.Context) error) error {
	return run(context.Background())
}

func serviceCommand(string) error {
	return errors.New("background mode is Windows-only for now; on Linux use a systemd unit")
}

type background struct {
	port    int
	service bool
}

func (background) Status() app.BackgroundStatus {
	return app.BackgroundStatus{Note: "Windows only for now"}
}
func (background) Install(func()) error { return errors.New("Windows only for now") }
func (background) Remove(func()) error  { return errors.New("Windows only for now") }

func spawnServiceRestart(string) error { return nil }
func restartServiceHelper() error      { return errors.New("Windows only") }
