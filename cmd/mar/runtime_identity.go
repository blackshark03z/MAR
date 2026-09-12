package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"mar/internal/store"
)

const releaseIdentityManifestName = "release-manifest.json"

// These may be overridden by release builds with -ldflags -X.
var embeddedReleaseVersion = "dev"
var embeddedBuildTimestamp = ""

type runtimeIdentity struct {
	ReleaseVersion          string   `json:"release_version"`
	SourceRevision          string   `json:"source_revision,omitempty"`
	SourceModified          bool     `json:"source_modified"`
	ProductionTreeIdentity  string   `json:"production_tree_identity,omitempty"`
	BinarySHA256            string   `json:"binary_sha256,omitempty"`
	GoVersion               string   `json:"go_version,omitempty"`
	BuildTimestamp          string   `json:"build_timestamp,omitempty"`
	BuildTimestampSource    string   `json:"build_timestamp_source,omitempty"`
	SQLiteSchemaSupported   int      `json:"sqlite_schema_supported"`
	SQLiteSchemaActive      int      `json:"sqlite_schema_active,omitempty"`
	UIAssetSHA256           string   `json:"ui_asset_sha256,omitempty"`
	ManifestPath            string   `json:"manifest_path,omitempty"`
	ManifestStatus          string   `json:"manifest_status"`
	Status                  string   `json:"status"`
	TrustedForRelease       bool     `json:"trusted_for_release"`
	Reasons                 []string `json:"reasons,omitempty"`
}

type releaseIdentityManifest struct {
	Schema                  string `json:"schema"`
	ReleaseVersion          string `json:"release_version"`
	GeneratedAt             string `json:"generated_at"`
	SourceRevision          string `json:"source_revision"`
	ProductionTreeIdentity  string `json:"production_tree_identity"`
	BinarySHA256            string `json:"binary_sha256"`
	GoVersion               string `json:"go_version"`
	BuildTimestamp          string `json:"build_timestamp"`
	BuildTimestampSource    string `json:"build_timestamp_source"`
	SQLiteSchemaSupported   int    `json:"sqlite_schema_supported"`
	UIAssetSHA256           string `json:"ui_asset_sha256"`
}

func currentRuntimeIdentityBase(executable string) (runtimeIdentity, error) {
	id := runtimeIdentity{
		ReleaseVersion:        strings.TrimSpace(embeddedReleaseVersion),
		SQLiteSchemaSupported: store.SupportedSchemaVersion(),
		ManifestStatus:        "NOT_CHECKED",
		Status:                "UNBOUND",
	}
	if id.ReleaseVersion == "" {
		id.ReleaseVersion = "dev"
	}
	if info, ok := debug.ReadBuildInfo(); ok && info != nil {
		id.GoVersion = strings.TrimSpace(info.GoVersion)
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				id.SourceRevision = strings.TrimSpace(setting.Value)
			case "vcs.modified":
				id.SourceModified = strings.EqualFold(strings.TrimSpace(setting.Value), "true")
			}
		}
	}
	if id.SourceRevision != "" {
		id.ProductionTreeIdentity = "git:" + id.SourceRevision
		if id.SourceModified {
			id.ProductionTreeIdentity += "+modified"
		}
	}
	binaryHash, err := sha256File(executable)
	if err != nil {
		return runtimeIdentity{}, err
	}
	id.BinarySHA256 = binaryHash
	uiHash := sha256.Sum256([]byte(ownerUIContractText()))
	id.UIAssetSHA256 = strings.ToUpper(hex.EncodeToString(uiHash[:]))
	if ts := strings.TrimSpace(embeddedBuildTimestamp); ts != "" {
		id.BuildTimestamp = ts
		id.BuildTimestampSource = "ldflags"
	} else if stat, statErr := os.Stat(executable); statErr == nil {
		id.BuildTimestamp = stat.ModTime().UTC().Format(time.RFC3339Nano)
		id.BuildTimestampSource = "executable_mtime"
	}
	return id, nil
}

func collectRuntimeIdentity(ctx context.Context, executable, dataRoot string, db *store.SQLite) runtimeIdentity {
	id, err := currentRuntimeIdentityBase(executable)
	if err != nil {
		return runtimeIdentity{ReleaseVersion: strings.TrimSpace(embeddedReleaseVersion), ManifestStatus: "UNAVAILABLE", Status: "IDENTITY_ERROR", Reasons: []string{err.Error()}}
	}
	if db != nil {
		if active, schemaErr := db.SchemaVersion(ctx); schemaErr != nil {
			id.Reasons = append(id.Reasons, "SQLITE_SCHEMA_UNAVAILABLE: "+schemaErr.Error())
		} else {
			id.SQLiteSchemaActive = active
			if active > id.SQLiteSchemaSupported {
				id.Reasons = append(id.Reasons, fmt.Sprintf("SQLITE_SCHEMA_NEWER_THAN_RUNTIME:%d>%d", active, id.SQLiteSchemaSupported))
			}
		}
	}
	manifestPath := filepath.Join(dataRoot, "runtime", releaseIdentityManifestName)
	id.ManifestPath = manifestPath
	data, readErr := os.ReadFile(manifestPath)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			id.ManifestStatus = "MISSING"
			id.Status = "MANIFEST_MISSING"
			id.Reasons = append(id.Reasons, "RELEASE_MANIFEST_MISSING")
		} else {
			id.ManifestStatus = "UNREADABLE"
			id.Status = "MANIFEST_UNREADABLE"
			id.Reasons = append(id.Reasons, "RELEASE_MANIFEST_UNREADABLE: "+readErr.Error())
		}
		appendBuildTrustReasons(&id)
		return id
	}
	var manifest releaseIdentityManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		id.ManifestStatus = "INVALID"
		id.Status = "MANIFEST_INVALID"
		id.Reasons = append(id.Reasons, "RELEASE_MANIFEST_INVALID: "+err.Error())
		appendBuildTrustReasons(&id)
		return id
	}
	reconcileReleaseManifest(&id, manifest)
	return id
}

func appendBuildTrustReasons(id *runtimeIdentity) {
	if strings.TrimSpace(id.SourceRevision) == "" {
		id.Reasons = append(id.Reasons, "BINARY_UNBOUND_NO_VCS_REVISION")
	}
	if id.SourceModified {
		id.Reasons = append(id.Reasons, "BINARY_SOURCE_MODIFIED")
	}
}

func reconcileReleaseManifest(id *runtimeIdentity, manifest releaseIdentityManifest) {
	id.ManifestStatus = "ALIGNED"
	id.Status = "ALIGNED"
	if strings.TrimSpace(manifest.ReleaseVersion) != "" {
		id.ReleaseVersion = strings.TrimSpace(manifest.ReleaseVersion)
	}
	mismatch := make([]string, 0, 8)
	if manifest.Schema != "mar-release-identity-v1" {
		mismatch = append(mismatch, "MANIFEST_SCHEMA_MISMATCH")
	}
	if !strings.EqualFold(strings.TrimSpace(manifest.SourceRevision), strings.TrimSpace(id.SourceRevision)) {
		mismatch = append(mismatch, "SOURCE_REVISION_MISMATCH")
	}
	if manifest.ProductionTreeIdentity != id.ProductionTreeIdentity {
		mismatch = append(mismatch, "PRODUCTION_TREE_IDENTITY_MISMATCH")
	}
	if !strings.EqualFold(strings.TrimSpace(manifest.BinarySHA256), strings.TrimSpace(id.BinarySHA256)) {
		mismatch = append(mismatch, "BINARY_SHA256_MISMATCH")
	}
	if strings.TrimSpace(manifest.GoVersion) != strings.TrimSpace(id.GoVersion) {
		mismatch = append(mismatch, "GO_VERSION_MISMATCH")
	}
	if manifest.SQLiteSchemaSupported != id.SQLiteSchemaSupported {
		mismatch = append(mismatch, "SQLITE_SCHEMA_SUPPORT_MISMATCH")
	}
	if !strings.EqualFold(strings.TrimSpace(manifest.UIAssetSHA256), strings.TrimSpace(id.UIAssetSHA256)) {
		mismatch = append(mismatch, "UI_ASSET_IDENTITY_MISMATCH")
	}
	if embedded := strings.TrimSpace(embeddedReleaseVersion); embedded != "" && embedded != "dev" && manifest.ReleaseVersion != embedded {
		mismatch = append(mismatch, "RELEASE_VERSION_MISMATCH")
	}
	appendBuildTrustReasons(id)
	if len(mismatch) > 0 {
		id.ManifestStatus = "MISMATCH"
		id.Status = "MANIFEST_MISMATCH"
		id.Reasons = append(id.Reasons, mismatch...)
		return
	}
	if len(id.Reasons) > 0 {
		id.Status = "UNTRUSTED_BUILD"
		return
	}
	id.TrustedForRelease = true
}

func writeCurrentReleaseManifest(releaseVersion, executable, outPath string) (releaseIdentityManifest, error) {
	releaseVersion = strings.TrimSpace(releaseVersion)
	if releaseVersion == "" {
		return releaseIdentityManifest{}, errors.New("release version is required")
	}
	id, err := currentRuntimeIdentityBase(executable)
	if err != nil {
		return releaseIdentityManifest{}, err
	}
	if id.SourceRevision == "" {
		return releaseIdentityManifest{}, errors.New("release manifest requires VCS-bound build metadata")
	}
	if id.SourceModified {
		return releaseIdentityManifest{}, errors.New("release manifest requires an unmodified source build")
	}
	manifest := releaseIdentityManifest{
		Schema:                 "mar-release-identity-v1",
		ReleaseVersion:         releaseVersion,
		GeneratedAt:            time.Now().UTC().Format(time.RFC3339Nano),
		SourceRevision:         id.SourceRevision,
		ProductionTreeIdentity: id.ProductionTreeIdentity,
		BinarySHA256:           id.BinarySHA256,
		GoVersion:              id.GoVersion,
		BuildTimestamp:         id.BuildTimestamp,
		BuildTimestampSource:   id.BuildTimestampSource,
		SQLiteSchemaSupported:  id.SQLiteSchemaSupported,
		UIAssetSHA256:          id.UIAssetSHA256,
	}
	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return releaseIdentityManifest{}, err
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return releaseIdentityManifest{}, fmt.Errorf("create release manifest directory: %w", err)
	}
	tmp := outPath + ".tmp"
	if err := os.WriteFile(tmp, append(payload, '\n'), 0o644); err != nil {
		return releaseIdentityManifest{}, fmt.Errorf("write release manifest: %w", err)
	}
	if err := os.Rename(tmp, outPath); err != nil {
		_ = os.Remove(tmp)
		return releaseIdentityManifest{}, fmt.Errorf("promote release manifest: %w", err)
	}
	return manifest, nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open executable for SHA-256: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash executable: %w", err)
	}
	return strings.ToUpper(hex.EncodeToString(h.Sum(nil))), nil
}
