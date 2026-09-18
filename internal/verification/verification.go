package verification

// ResearchArtifactProfileID is the language-neutral profile for research,
// documentation, eval datasets and other bounded non-executable artifacts.
const ResearchArtifactProfileID = "research-artifacts"

// ResearchArtifactProfile relies on candidate-path admission plus criterion-bound evidence.
func ResearchArtifactProfile() Profile {
	return Profile{ID: ResearchArtifactProfileID, ChangeScope: ChangeScopeResearchArtifacts}
}
