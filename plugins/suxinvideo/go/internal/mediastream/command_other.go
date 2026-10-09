//go:build !windows

package mediastream

import "os/exec"

func configureCommand(command *exec.Cmd) {}
