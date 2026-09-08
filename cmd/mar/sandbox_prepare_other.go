//go:build !windows

package main

import (
	"context"
	"errors"
)

func runElevatedSandboxPrepare(context.Context, string, string) error {
	return errors.New("elevated sandbox preparation is supported only on Windows")
}
