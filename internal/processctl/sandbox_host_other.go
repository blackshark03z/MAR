//go:build !windows

package processctl

import (
	"context"
	"errors"
)

var ErrSandboxHostNotPrepared = errors.New("Windows sandbox host is not prepared")

func CheckSandboxHostReady(context.Context, string) error {
	return ErrSandboxHostNotPrepared
}

func PrepareSandboxHost() error {
	return errors.New("Windows AppContainer sandbox host preparation is only supported on Windows")
}
