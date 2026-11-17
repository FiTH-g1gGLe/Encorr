//go:build !windows

package main

import (
	"os/exec"
	"runtime"
)

func openBrowser(url string) {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	_ = exec.Command(name, url).Start()
}
