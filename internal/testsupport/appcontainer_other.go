//go:build !windows

package testsupport

func InAppContainer() (bool, error) { return false, nil }
