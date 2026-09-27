//go:build !unix && !windows

package proctree

import (
	"os"
	"os/exec"
)

func configureProcess(cmd *exec.Cmd) {}
func attachProcess(p *os.Process) (Control, error) {
	kill := func() { _ = p.Kill() }
	return Control{kill, kill, func() {}}, nil
}
