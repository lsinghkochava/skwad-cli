package models

// DefaultMCPPort is the default port for the MCP server.
const DefaultMCPPort = 8777

// AppearanceMode controls light/dark theme selection.
type AppearanceMode string

const (
	AppearanceModeAuto   AppearanceMode = "auto"
	AppearanceModeSystem AppearanceMode = "system"
	AppearanceModeLight  AppearanceMode = "light"
	AppearanceModeDark   AppearanceMode = "dark"
)

// AutopilotAction controls what the autopilot does when an agent goes idle.
type AutopilotAction string

const (
	AutopilotActionMark     AutopilotAction = "mark"
	AutopilotActionAsk      AutopilotAction = "ask"
	AutopilotActionContinue AutopilotAction = "continue"
	AutopilotActionCustom   AutopilotAction = "custom"
)

// AutopilotProvider selects which LLM backs the autopilot.
type AutopilotProvider string

const (
	AutopilotProviderOpenAI    AutopilotProvider = "openai"
	AutopilotProviderAnthropic AutopilotProvider = "anthropic"
	AutopilotProviderGoogle    AutopilotProvider = "google"
)

// AgentTypeOptions holds per-agent-type CLI option overrides.
type AgentTypeOptions struct {
	ClaudeOptions    string `json:"claudeOptions"`
	CodexOptions     string `json:"codexOptions"`
	OpenCodeOptions  string `json:"opencodeOptions"`
	GeminiOptions    string `json:"geminiOptions"`
	CopilotOptions   string `json:"copilotOptions"`
	Custom1Command   string `json:"custom1Command"`
	Custom1Options   string `json:"custom1Options"`
	Custom2Command   string `json:"custom2Command"`
	Custom2Options   string `json:"custom2Options"`
}

// AutopilotSettings configures the autopilot feature.
type AutopilotSettings struct {
	Enabled      bool              `json:"enabled"`
	Provider     AutopilotProvider `json:"provider"`
	APIKey       string            `json:"apiKey"`
	Action       AutopilotAction   `json:"action"`
	CustomPrompt string            `json:"customPrompt"`
}

// VoiceSettings configures voice input.
type VoiceSettings struct {
	Enabled       bool   `json:"enabled"`
	PushToTalkKey string `json:"pushToTalkKey"` // default: "Right Shift"
	AutoInsert    bool   `json:"autoInsert"`
}

// AppSettings holds all application configuration.
type AppSettings struct {
	// General
	RestoreLayoutOnLaunch bool           `json:"restoreLayoutOnLaunch"`
	KeepInTray            bool           `json:"keepInTray"`
	SourceBaseFolder      string         `json:"sourceBaseFolder"`

	// Terminal appearance
	TerminalFontName  string `json:"terminalFontName"`
	TerminalFontSize  int    `json:"terminalFontSize"`
	TerminalBgColor   string `json:"terminalBgColor"`   // hex
	TerminalFgColor   string `json:"terminalFgColor"`   // hex

	// Coding
	DefaultOpenWithApp string           `json:"defaultOpenWithApp"`
	AgentTypeOptions   AgentTypeOptions `json:"agentTypeOptions"`

	// MCP server
	MCPServerEnabled bool `json:"mcpServerEnabled"`
	MCPServerPort    int  `json:"mcpServerPort"`

	// Autopilot
	Autopilot AutopilotSettings `json:"autopilot"`

	// Voice
	Voice VoiceSettings `json:"voice"`

	// Appearance
	AppearanceMode    AppearanceMode `json:"appearanceMode"`
	MarkdownFontSize  int            `json:"markdownFontSize"`
	MermaidTheme      string         `json:"mermaidTheme"` // auto, light, dark
	MermaidScale      float64        `json:"mermaidScale"`

	// Notifications
	NotificationsEnabled bool `json:"notificationsEnabled"`
}

// DefaultSettings returns AppSettings populated with sensible defaults.
func DefaultSettings() AppSettings {
	return AppSettings{
		RestoreLayoutOnLaunch: true,
		KeepInTray:            true,
		TerminalFontName:      "monospace",
		TerminalFontSize:      13,
		TerminalBgColor:       "#1E1E1E",
		TerminalFgColor:       "#D4D4D4",
		MCPServerEnabled:      true,
		MCPServerPort:         DefaultMCPPort,
		Autopilot: AutopilotSettings{
			Action: AutopilotActionMark,
		},
		Voice: VoiceSettings{
			PushToTalkKey: "Right Shift",
			AutoInsert:    true,
		},
		AppearanceMode:       AppearanceModeAuto,
		MarkdownFontSize:     14,
		MermaidTheme:         "auto",
		MermaidScale:         1.0,
		NotificationsEnabled: true,
	}
}
