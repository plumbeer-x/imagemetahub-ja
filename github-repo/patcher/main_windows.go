//go:build windows

package main

import "syscall"

func setConsoleUTF8() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setCP := kernel32.NewProc("SetConsoleOutputCP")
	setCP.Call(65001)
	setInCP := kernel32.NewProc("SetConsoleCP")
	setInCP.Call(65001)
}
