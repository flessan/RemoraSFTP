//go:build windows

package tui

import "os"

// Windows consoles do not support POSIX non-blocking mode on stdin; the read
// loop falls back to a plain blocking read (a trailing lone ESC waits for the
// next keystroke to be interpreted, which is acceptable there).
func enableNonBlocking(fd uintptr) bool { return false }

func isWouldBlock(err error) bool { return false }

// notifyWinch is a no-op on Windows; the terminal has no resize signal the
// process can subscribe to.
func notifyWinch(ch chan os.Signal) {}
