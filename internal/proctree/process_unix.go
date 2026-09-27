//go:build unix

package proctree

import (
	"os"
	"os/exec"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func attachProcess(p *os.Process) (Control, error) {
	kill := func() { _ = syscall.Kill(-p.Pid, syscall.SIGKILL) }
	return Control{func() { _ = syscall.Kill(-p.Pid, syscall.SIGTERM) }, kill, kill}, nil
}
