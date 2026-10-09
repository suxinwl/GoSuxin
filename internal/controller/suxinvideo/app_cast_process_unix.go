//go:build !windows

package suxinvideo

import "os/exec"

func castProcessHidden(command *exec.Cmd) {}
