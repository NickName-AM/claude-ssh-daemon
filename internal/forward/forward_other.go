//go:build !linux

package forward

import (
	"os/exec"
	"syscall"
)

// setSysProcAttr sets macOS (and other non-Linux) orphan prevention on cmd (D-12).
// Setpgid: true puts the child process in its own process group.
// On shutdown, daemon.Run calls KillAll() which sends SIGKILL directly to each
// live process via cmd.Process.Kill() (D-07).
//
// The linux counterpart (forward_linux.go) uses Pdeathsig: syscall.SIGTERM instead,
// which is not defined in syscall.SysProcAttr on darwin (Pitfall 1).
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
