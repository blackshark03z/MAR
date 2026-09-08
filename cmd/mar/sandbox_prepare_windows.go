//go:build windows

package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func runElevatedSandboxPrepare(ctx context.Context, executable, workspace string) error {
	script := `$ErrorActionPreference='Stop'; ` +
		`$p=Start-Process -FilePath $env:MAR_SANDBOX_PREPARE_EXE ` +
		`-ArgumentList @('sandbox-host-prepare','-workspace',$env:MAR_SANDBOX_PREPARE_WORKSPACE) ` +
		`-Verb RunAs -Wait -PassThru; if($p.ExitCode -ne 0){exit $p.ExitCode}`
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(cmd.Environ(),
		"MAR_SANDBOX_PREPARE_EXE="+executable,
		"MAR_SANDBOX_PREPARE_WORKSPACE="+workspace,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("Windows UAC sandbox preparation did not complete: %s", message)
	}
	return nil
}
