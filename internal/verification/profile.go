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

type Profile struct {
	ID       string    `json:"id"`
	Commands []Command `json:"commands"`
}

func (p Profile) Validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return errors.New("verification profile id is required")
	}
	if len(p.Commands) == 0 {
		return errors.New("verification profile requires at least one command")
	}
	for _, command := range p.Commands {
		name := strings.ToLower(filepath.Base(strings.TrimSpace(command.Name)))
		if name != "go" && name != "go.exe" {
			return fmt.Errorf("verification command %q is not supported", command.Name)
		}
		if len(command.Args) == 0 {
			return errors.New("verification go command requires subcommand")
		}
		sub := strings.ToLower(command.Args[0])
		if sub != "test" && sub != "vet" && sub != "build" {
			return fmt.Errorf("verification go subcommand %q is not allowed", sub)
		}
	}
	return nil
}

func (p Profile) Hash() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	clone := Profile{ID: strings.TrimSpace(p.ID), Commands: make([]Command, len(p.Commands))}
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
	clone := Profile{ID: profile.ID, Commands: make([]Command, len(profile.Commands))}
	for i, command := range profile.Commands {
		clone.Commands[i] = Command{Name: command.Name, Args: append([]string(nil), command.Args...), Cwd: command.Cwd}
	}
	return clone
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
