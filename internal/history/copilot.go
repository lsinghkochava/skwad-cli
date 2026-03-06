package history

// CopilotProvider is a stub history provider for GitHub Copilot CLI.
//
// The `gh copilot` tool operates as a one-shot command and does not persist
// conversation sessions to the local filesystem in an accessible format.
// This provider satisfies the Provider interface but always returns empty results.
type CopilotProvider struct{}

// ListSessions always returns nil; Copilot CLI has no local session history.
func (p *CopilotProvider) ListSessions(_ string) ([]SessionSummary, error) {
	return nil, nil
}

// DeleteSession is a no-op; there are no local session files to remove.
func (p *CopilotProvider) DeleteSession(_ string) error {
	return nil
}
