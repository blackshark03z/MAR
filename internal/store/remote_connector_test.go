package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestRemoteConnectorProfilesPersistAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mar.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Round(0)
	want := RemoteConnectorProfile{ID: RemoteConnectorClaudeWeb, StableBaseURL: "https://mar.example.com", PathToken: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", PreferredMode: RemoteConnectorModeStable, UpdatedAt: now}
	if err := db.UpsertRemoteConnectorProfile(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err := db.GetRemoteConnectorProfile(context.Background(), RemoteConnectorClaudeWeb)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.StableBaseURL != want.StableBaseURL || got.PathToken != want.PathToken || got.PreferredMode != want.PreferredMode || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("profile did not survive restart: got=%+v want=%+v", got, want)
	}
}

func TestRemoteConnectorProfilesAreIndependent(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "mar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	profiles := []RemoteConnectorProfile{
		{ID: RemoteConnectorClaudeWeb, StableBaseURL: "https://claude.example.com", PathToken: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PreferredMode: RemoteConnectorModeStable, UpdatedAt: now},
		{ID: RemoteConnectorChatGPTWeb, StableBaseURL: "https://gpt.example.com", PathToken: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", PreferredMode: RemoteConnectorModeTemporary, UpdatedAt: now},
	}
	for _, p := range profiles {
		if err := db.UpsertRemoteConnectorProfile(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.ListRemoteConnectorProfiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d profiles", len(got))
	}
	if got[0].PathToken == got[1].PathToken || got[0].StableBaseURL == got[1].StableBaseURL {
		t.Fatalf("connector profiles collapsed together: %+v", got)
	}
}
