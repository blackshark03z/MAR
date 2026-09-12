package main

import (
	"slices"
	"testing"
)

func alignedIdentityFixture() runtimeIdentity {
	return runtimeIdentity{
		ReleaseVersion:         "dev",
		SourceRevision:         "abc123",
		ProductionTreeIdentity: "git:abc123",
		BinarySHA256:           "AABBCC",
		GoVersion:              "go1.27.0",
		SQLiteSchemaSupported:  14,
		UIAssetSHA256:          "DDEEFF",
	}
}

func alignedManifestFixture() releaseIdentityManifest {
	return releaseIdentityManifest{
		Schema:                 "mar-release-identity-v1",
		ReleaseVersion:         "v1.2.0",
		SourceRevision:         "abc123",
		ProductionTreeIdentity: "git:abc123",
		BinarySHA256:           "AABBCC",
		GoVersion:              "go1.27.0",
		SQLiteSchemaSupported:  14,
		UIAssetSHA256:          "DDEEFF",
	}
}

func TestReleaseManifestAlignmentProducesTrustedRuntimeIdentity(t *testing.T) {
	id := alignedIdentityFixture()
	reconcileReleaseManifest(&id, alignedManifestFixture())
	if id.Status != "ALIGNED" || id.ManifestStatus != "ALIGNED" || !id.TrustedForRelease || id.ReleaseVersion != "v1.2.0" {
		t.Fatalf("expected aligned trusted identity, got %+v", id)
	}
}

func TestReleaseManifestMismatchFailsClosed(t *testing.T) {
	id := alignedIdentityFixture()
	manifest := alignedManifestFixture()
	manifest.BinarySHA256 = "STALE"
	reconcileReleaseManifest(&id, manifest)
	if id.Status != "MANIFEST_MISMATCH" || id.ManifestStatus != "MISMATCH" || id.TrustedForRelease {
		t.Fatalf("mismatched artifact must not be trusted: %+v", id)
	}
	if !slices.Contains(id.Reasons, "BINARY_SHA256_MISMATCH") {
		t.Fatalf("missing binary mismatch reason: %+v", id.Reasons)
	}
}

func TestReleaseManifestDirtyBuildNeverBecomesTrusted(t *testing.T) {
	id := alignedIdentityFixture()
	id.SourceModified = true
	id.ProductionTreeIdentity = "git:abc123+modified"
	manifest := alignedManifestFixture()
	manifest.ProductionTreeIdentity = id.ProductionTreeIdentity
	reconcileReleaseManifest(&id, manifest)
	if id.TrustedForRelease || id.Status != "UNTRUSTED_BUILD" {
		t.Fatalf("dirty source build must not become release trusted: %+v", id)
	}
	if !slices.Contains(id.Reasons, "BINARY_SOURCE_MODIFIED") {
		t.Fatalf("missing dirty-build reason: %+v", id.Reasons)
	}
}
