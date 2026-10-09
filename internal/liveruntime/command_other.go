//go:build !windows

package liveruntime

import (
	"os/exec"
	"syscall"
)

func configureCommand(command *exec.Cmd) { command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func ownProcess(command *exec.Cmd) (func(), error) {
	return func() { _ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL) }, nil
}
