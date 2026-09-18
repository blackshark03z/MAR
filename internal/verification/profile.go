package verification

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type Command struct {
	Name string   `json:"name"`
	Args []string `json:"args"`
	Cwd  string   `json:"cwd,omitempty"`
}

type ChangeScope string

const (
	ChangeScopeAny               ChangeScope = "any"
	ChangeScopeDocumentationOnly ChangeScope = "documentation-only"
	ChangeScopeResearchArtifacts ChangeScope = "research-artifacts"
)

type Profile struct {
	ID          string      `json:"id"`
	ChangeScope ChangeScope `json:"change_scope,omitempty"`
	Commands    []Command   `json:"commands"`
}

func (p Profile) EffectiveChangeScope() ChangeScope {
	if p.ChangeScope == "" {
		return ChangeScopeAny
	}
	return p.ChangeScope
}

func (p Profile) Validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return errors.New("verification profile id is required")
	}
	switch p.EffectiveChangeScope() {
	case ChangeScopeAny, ChangeScopeDocumentationOnly, ChangeScopeResearchArtifacts:
	default:
		return fmt.Errorf("verification profile change scope %q is not supported", p.ChangeScope)
	}
	if len(p.Commands) == 0 && p.EffectiveChangeScope() != ChangeScopeResearchArtifacts {
		return errors.New("verification profile requires at least one command")
	}
	for _, command := range p.Commands {
		name := strings.ToLower(filepath.Base(strings.TrimSpace(command.Name)))
		switch name {
		case "go", "go.exe":
			if len(command.Args) == 0 {
				return errors.New("verification go command requires subcommand")
			}
			sub := strings.ToLower(command.Args[0])
			if sub != "test" && sub != "vet" && sub != "build" {
				return fmt.Errorf("verification go subcommand %q is not allowed", sub)
			}
		case "python", "python.exe":
			if len(command.Args) < 2 || command.Args[0] != "-m" {
				return errors.New("verification Python command must use an allowed standard module")
			}
			module := strings.ToLower(strings.TrimSpace(command.Args[1]))
			if module != "unittest" && module != "compileall" {
				return fmt.Errorf("verification Python module %q is not allowed", command.Args[1])
			}
		default:
			return fmt.Errorf("verification command %q is not supported", command.Name)
		}
	}
	return nil
}

func (p Profile) Hash() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	clone := Profile{ID: strings.TrimSpace(p.ID), ChangeScope: p.EffectiveChangeScope(), Commands: make([]Command, len(p.Commands))}
	for i, command := range p.Commands {
		clone.Commands[i] = Command{Name: strings.TrimSpace(command.Name), Args: append([]string(nil), command.Args...), Cwd: filepath.ToSlash(filepath.Clean(command.Cwd))}
	}
	payload, err := json.Marshal(clone)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

type Registry struct {
	profiles map[string]Profile
}

func NewRegistry(profiles ...Profile) (*Registry, error) {
	registry := &Registry{profiles: make(map[string]Profile, len(profiles))}
	for _, profile := range profiles {
		if err := profile.Validate(); err != nil {
			return nil, err
		}
		id := strings.TrimSpace(profile.ID)
		if _, duplicate := registry.profiles[id]; duplicate {
			return nil, fmt.Errorf("duplicate verification profile %q", id)
		}
		registry.profiles[id] = cloneProfile(profile)
	}
	return registry, nil
}

func (r *Registry) Get(id string) (Profile, bool) {
	if r == nil {
		return Profile{}, false
	}
	profile, ok := r.profiles[strings.TrimSpace(id)]
	if !ok {
		return Profile{}, false
	}
	return cloneProfile(profile), true
}

func cloneProfile(profile Profile) Profile {
	clone := Profile{ID: profile.ID, ChangeScope: profile.ChangeScope, Commands: make([]Command, len(profile.Commands))}
	for i, command := range profile.Commands {
		clone.Commands[i] = Command{Name: command.Name, Args: append([]string(nil), command.Args...), Cwd: command.Cwd}
	}
	return clone
}

func (p Profile) ValidateChangedPaths(paths []string) error {
	switch p.EffectiveChangeScope() {
	case ChangeScopeAny:
		return nil
	case ChangeScopeDocumentationOnly:
		for _, changed := range paths {
			if !isDocumentationPath(changed) {
				return fmt.Errorf("verification profile %q only admits documentation changes; observed %q", p.ID, changed)
			}
		}
	case ChangeScopeResearchArtifacts:
		for _, changed := range paths {
			if !isResearchArtifactPath(changed) {
				return fmt.Errorf("verification profile %q only admits non-executable research/artifact changes; observed %q", p.ID, changed)
			}
		}
	}
	return nil
}

func isDocumentationPath(changed string) bool {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(changed)))
	base := strings.ToLower(filepath.Base(clean))
	switch base {
	case "license", "notice", "authors", "contributors", "changelog":
		return true
	}
	switch strings.ToLower(filepath.Ext(clean)) {
	case ".md", ".mdx", ".rst", ".adoc", ".asciidoc", ".txt":
		return true
	default:
		return false
	}
}

func isResearchArtifactPath(changed string) bool {
	if isDocumentationPath(changed) {
		return true
	}
	clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(changed)))
	switch strings.ToLower(filepath.Ext(clean)) {
	case ".json", ".jsonl", ".yaml", ".yml", ".toml", ".csv", ".tsv":
		return true
	default:
		return false
	}
}

func (r *Registry) IDs() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.profiles))
	for id := range r.profiles {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
