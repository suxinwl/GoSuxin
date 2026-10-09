//go:build windows

package suxinvideo

import (
	"os/exec"
	"syscall"
)

func castProcessHidden(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
