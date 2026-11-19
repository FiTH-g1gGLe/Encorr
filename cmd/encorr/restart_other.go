//go:build !windows

package main

import (
	"os"
	"syscall"
)

// restartSelf replaces this process with the updated program, keeping the
// same process id so systemd and terminals carry on as before.
func restartSelf(exe string, args []string) error {
	return syscall.Exec(exe, append([]string{exe}, args...), os.Environ())
}
