package main

import (
	"os"
	"os/exec"
)

// restartSelf starts the (updated) program again in this console window,
// then this copy exits.
func restartSelf(exe string, args []string) error {
	cmd := exec.Command(exe, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Start()
}
