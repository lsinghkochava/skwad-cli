package agent

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/models"
)

// RegistrationPrompt returns the text to inject into a terminal after startup
// so the agent calls register-agent. Returns "" for agent types that don't
// need a prompt or when MCP is disabled.
func RegistrationPrompt(agentID uuid.UUID, mcpURL string, agentType models.AgentType) string {
	if mcpURL == "" {
		return ""
	}
	if agentType == models.AgentTypeShell {
		return ""
	}

	base := fmt.Sprintf(
		"Your agent ID is %s. The Skwad MCP server is at %s. "+
			"Please call the register-agent tool now with your agentId, name, and folder.",
		agentID.String(), mcpURL,
	)
	return base
}
