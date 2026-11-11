package source

// IsGitHubSource identifies the backend without making any request.
func IsGitHubSource(src Source) bool {
	_, ok := src.(*githubSource)
	return ok
}

// SetAuditGitHubEndpoint confines audit repros to their local server.
func SetAuditGitHubEndpoint(src Source, url string) {
	github := src.(*githubSource)
	github.client.SetBaseURL(url)
	github.client.SetRetryCount(0)
}
