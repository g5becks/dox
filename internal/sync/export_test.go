package sync

// Test-only exports for internal helper functions.

//nolint:gochecknoglobals // Test-only exports
var (
	ResolveSourceNames     = resolveSourceNames
	ResolveGitHubToken     = resolveGitHubToken
	ResolveOutputRoot      = resolveOutputRoot
	ResolveSourceOutputDir = resolveSourceOutputDir
)
