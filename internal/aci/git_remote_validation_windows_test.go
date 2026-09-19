//go:build windows

package aci

import "testing"

func TestRemoteGitInputValidation(t *testing.T) {
	for _, remote := range []string{"origin", "upstream-1", "corp_mirror.git"} {
		if err := validateRemoteName(remote); err != nil {
			t.Fatalf("valid remote %q rejected: %v", remote, err)
		}
	}
	for _, remote := range []string{"", "-origin", "../origin", "https://example.com/repo.git", "origin:evil"} {
		if err := validateRemoteName(remote); err == nil {
			t.Fatalf("invalid remote %q accepted", remote)
		}
	}

	for _, branch := range []string{"master", "main", "feature/safe-1", "release/v1.2"} {
		if err := validateRemoteBranch(branch); err != nil {
			t.Fatalf("valid branch %q rejected: %v", branch, err)
		}
	}
	for _, branch := range []string{"", "-force", "/main", "main/", "../main", "main..old", "main//x", "main@{1}", "bad:ref", "refs.lock"} {
		if err := validateRemoteBranch(branch); err == nil {
			t.Fatalf("invalid branch %q accepted", branch)
		}
	}
}

func TestConfiguredRemoteAddressValidation(t *testing.T) {
	for _, remote := range []string{
		"https://github.com/example/repo.git",
		"ssh://git@github.com/example/repo.git",
		"git@github.com:example/repo.git",
	} {
		if err := validateConfiguredRemoteAddress(remote); err != nil {
			t.Fatalf("valid network remote %q rejected: %v", remote, err)
		}
	}
	for _, remote := range []string{
		"file:///C:/repo",
		"C:\\repo",
		"https://localhost/repo.git",
		"https://127.0.0.1/repo.git",
		"https://user:secret@example.com/repo.git",
		"git@localhost:repo.git",
	} {
		if err := validateConfiguredRemoteAddress(remote); err == nil {
			t.Fatalf("unsafe/non-network remote %q accepted", remote)
		}
	}
}
