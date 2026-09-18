//go:build !windows

package tui

import (
	"os"
	"os/signal"
	"syscall"
)

// enableNonBlocking puts stdin into non-blocking mode so the read loop can
// interleave a quiet-period timeout (used to flush a lone Escape key).
func enableNonBlocking(fd uintptr) bool {
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_SETFL, syscall.O_NONBLOCK)
	return errno == 0
}

// isWouldBlock reports whether a read error means "no data available yet".
func isWouldBlock(err error) bool {
	ne, ok := err.(syscall.Errno)
	return ok && (ne == syscall.EAGAIN || ne == syscall.EWOULDBLOCK)
}

// notifyWinch subscribes ch to terminal resize events.
func notifyWinch(ch chan os.Signal) {
	signal.Notify(ch, syscall.SIGWINCH)
}
