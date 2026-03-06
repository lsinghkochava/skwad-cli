// Package autopilot provides LLM-based analysis of agent output to drive
// automatic follow-up actions when agents go idle.
package autopilot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Jared-Boschmann/skwad-linux/internal/models"
)

// Classification is the result of analyzing an agent's last message.
type Classification string

const (
	ClassificationCompleted Classification = "completed"
	ClassificationBinary    Classification = "binary"
	ClassificationOpen      Classification = "open"
)

// Service analyses agent messages and takes automated actions.
type Service struct {
	settings *models.AutopilotSettings
	client   *http.Client
}

// NewService creates an autopilot service.
func NewService(settings *models.AutopilotSettings) *Service {
	return &Service{
		settings: settings,
		client:   &http.Client{},
	}
}

// Analyze classifies the agent's last message.
func (s *Service) Analyze(lastMessage string) (Classification, error) {
	if !s.settings.Enabled {
		return ClassificationCompleted, nil
	}

	prompt := fmt.Sprintf(
		"Classify this AI agent message. Reply with exactly one word: completed, binary, or open.\n\n"+
			"- completed: the agent finished work, no input needed\n"+
			"- binary: the agent is asking for simple yes/no approval\n"+
			"- open: the agent is asking an open-ended question\n\n"+
			"Message:\n%s", lastMessage,
	)

	response, err := s.callLLM(prompt)
	if err != nil {
		return ClassificationCompleted, err
	}

	switch response {
	case "completed":
		return ClassificationCompleted, nil
	case "binary":
		return ClassificationBinary, nil
	default:
		return ClassificationOpen, nil
	}
}

// CustomResponse calls the LLM with the custom prompt and the agent message,
// returning the LLM's reply to inject into the terminal.
func (s *Service) CustomResponse(agentMessage string) (string, error) {
	prompt := s.settings.CustomPrompt + "\n\nAgent message:\n" + agentMessage
	return s.callLLM(prompt)
}

func (s *Service) callLLM(prompt string) (string, error) {
	switch s.settings.Provider {
	case models.AutopilotProviderAnthropic:
		return s.callAnthropic(prompt)
	case models.AutopilotProviderOpenAI:
		return s.callOpenAI(prompt)
	case models.AutopilotProviderGoogle:
		return s.callGoogle(prompt)
	default:
		return "", fmt.Errorf("unknown provider: %s", s.settings.Provider)
	}
}

func (s *Service) callAnthropic(prompt string) (string, error) {
	body := map[string]interface{}{
		"model":      "claude-haiku-4-5",
		"max_tokens": 64,
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
	}
	return s.post("https://api.anthropic.com/v1/messages", body, func(data []byte) (string, error) {
		var resp struct {
			Content []struct{ Text string } `json:"content"`
		}
		if err := json.Unmarshal(data, &resp); err != nil || len(resp.Content) == 0 {
			return "", fmt.Errorf("parse anthropic response: %w", err)
		}
		return resp.Content[0].Text, nil
	}, map[string]string{
		"x-api-key":         s.settings.APIKey,
		"anthropic-version": "2023-06-01",
	})
}

func (s *Service) callOpenAI(prompt string) (string, error) {
	body := map[string]interface{}{
		"model":      "gpt-4o-mini",
		"max_tokens": 64,
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
	}
	return s.post("https://api.openai.com/v1/chat/completions", body, func(data []byte) (string, error) {
		var resp struct {
			Choices []struct {
				Message struct{ Content string } `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(data, &resp); err != nil || len(resp.Choices) == 0 {
			return "", fmt.Errorf("parse openai response: %w", err)
		}
		return resp.Choices[0].Message.Content, nil
	}, map[string]string{
		"Authorization": "Bearer " + s.settings.APIKey,
	})
}

func (s *Service) callGoogle(prompt string) (string, error) {
	url := "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash-lite:generateContent?key=" + s.settings.APIKey
	body := map[string]interface{}{
		"contents": []map[string]interface{}{
			{"parts": []map[string]string{{"text": prompt}}},
		},
		"generationConfig": map[string]interface{}{"maxOutputTokens": 64},
	}
	return s.post(url, body, func(data []byte) (string, error) {
		var resp struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal(data, &resp); err != nil || len(resp.Candidates) == 0 {
			return "", fmt.Errorf("parse google response: %w", err)
		}
		parts := resp.Candidates[0].Content.Parts
		if len(parts) == 0 {
			return "", fmt.Errorf("empty google response")
		}
		return parts[0].Text, nil
	}, nil)
}

func (s *Service) post(url string, body interface{}, parse func([]byte) (string, error), headers map[string]string) (string, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return parse(buf.Bytes())
}
