package main

import (
	"embed"
	"io/fs"
	"strings"
)

// ownerUIDist is the production React/Vite Owner Console bundle. The compiled
// assets are committed so normal Go builds never require Node at runtime.
//
//go:embed owner_ui_dist
var ownerUIDist embed.FS

var ownerUIFS = mustOwnerUISubFS()
var ownerUIHTML = mustOwnerUIIndex()

func mustOwnerUISubFS() fs.FS {
	sub, err := fs.Sub(ownerUIDist, "owner_ui_dist")
	if err != nil {
		panic(err)
	}
	return sub
}

func mustOwnerUIIndex() string {
	data, err := ownerUIDist.ReadFile("owner_ui_dist/index.html")
	if err != nil {
		panic(err)
	}
	return string(data)
}

// ownerUIContractText gives tests one bounded, deterministic representation of
// the production bundle without coupling them to Vite's content-hashed names.
func ownerUIContractText() string {
	var out strings.Builder
	_ = fs.WalkDir(ownerUIFS, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, readErr := fs.ReadFile(ownerUIFS, path)
		if readErr != nil {
			return readErr
		}
		out.Write(data)
		out.WriteByte('\n')
		return nil
	})
	return out.String()
}
