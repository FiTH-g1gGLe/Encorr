package main

import (
	"syscall"
	"unsafe"
)

// openBrowser asks Windows to open the link in the default browser, the
// same way clicking a link does (ShellExecute). It doesn't start any helper
// programs, which security software rightly treats as suspicious.
func openBrowser(url string) {
	shell32 := syscall.NewLazyDLL("shell32.dll")
	shellExecute := shell32.NewProc("ShellExecuteW")
	verb, _ := syscall.UTF16PtrFromString("open")
	target, err := syscall.UTF16PtrFromString(url)
	if err != nil {
		return
	}
	const swShowNormal = 1
	_, _, _ = shellExecute.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(target)), 0, 0, swShowNormal)
}
