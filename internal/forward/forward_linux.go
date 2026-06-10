//go:build linux

package forward

import (
	"os/exec"
	"syscall"
)

// setSysProcAttr sets Linux-specific orphan prevention on cmd (D-12).
// Pdeathsig: syscall.SIGTERM causes the kernel to send SIGTERM to the child
// process when its parent (the daemon) exits, preventing orphaned ssh -L
// subprocesses.
//
// Pdeathsig is defined only in syscall on linux and freebsd — NOT on darwin.
// This file is compiled only on linux via the //go:build constraint.
// The counterpart forward_other.go handles macOS and other platforms.
//
// runtime.GOOS check is not sufficient because the Go compiler evaluates all
// struct literals at compile time regardless of the enclosing branch (Pitfall 1).
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}
