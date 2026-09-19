package agent

import (
	"testing"

	"mar/internal/domain"
	"mar/internal/model"
)

func TestPinToolDefinitionsProjectsNetworkAndPushAuthority(t *testing.T) {
	defs := []model.ToolDefinition{
		{Name: "read_file"},
		{Name: "network_fetch"},
		{Name: "git_remote_ref"},
		{Name: "git_push_head"},
	}
	names := func(authority domain.Authority) map[string]bool {
		got, _, err := pinToolDefinitions(defs, authority)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, def := range got {
			out[def.Name] = true
		}
		return out
	}

	none := names(domain.Authority{})
	if none["network_fetch"] || none["git_remote_ref"] || none["git_push_head"] {
		t.Fatalf("network tools leaked without authority: %#v", none)
	}
	network := names(domain.Authority{NetworkAllowed: true})
	if !network["network_fetch"] || !network["git_remote_ref"] || network["git_push_head"] {
		t.Fatalf("unexpected network-only projection: %#v", network)
	}
	pushOnly := names(domain.Authority{RemoteGitWrite: true})
	if pushOnly["network_fetch"] || pushOnly["git_remote_ref"] || pushOnly["git_push_head"] {
		t.Fatalf("remote write alone must not imply network: %#v", pushOnly)
	}
	full := names(domain.Authority{NetworkAllowed: true, RemoteGitWrite: true})
	if !full["network_fetch"] || !full["git_remote_ref"] || !full["git_push_head"] {
		t.Fatalf("missing granted network/push tools: %#v", full)
	}
}
