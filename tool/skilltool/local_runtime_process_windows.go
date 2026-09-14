//go:build windows

package skilltool

import "os/exec"

func configureProcessGroup(_ *exec.Cmd) {}

func killProcessGroup(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return nil
	}
	return command.Process.Kill()
}
