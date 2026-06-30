package autopilot

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lsinghkochava/skwad-cli/internal/models"
)

// captureTransport records the last request it sees and returns a canned response.
type captureTransport struct {
	lastReq *http.Request
	body    string
}

func (c *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.lastReq = req
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewBufferString(c.body)),
		Header:     make(http.Header),
	}, nil
}

func newServiceForTest(settings *models.AutopilotSettings) (*Service, *captureTransport) {
	ct := &captureTransport{body: `{"content":[{"text":"completed"}]}`}
	s := &Service{
		settings: settings,
		client:   &http.Client{Transport: ct},
	}
	return s, ct
}

func TestAutopilotUsesEnvKeyNotSettings(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "env-key-123")

	s, ct := newServiceForTest(&models.AutopilotSettings{
		Enabled:  true,
		Provider: models.AutopilotProviderAnthropic,
		APIKey:   "settings-key-should-be-ignored",
	})

	if _, err := s.Analyze("agent done"); err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	got := ct.lastReq.Header.Get("x-api-key")
	if got != "env-key-123" {
		t.Errorf("x-api-key header = %q, want env value 'env-key-123'", got)
	}
}

func TestAutopilotForcesAnthropicRegardlessOfProvider(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "env-key")

	for _, prov := range []models.AutopilotProvider{
		models.AutopilotProviderOpenAI,
		models.AutopilotProviderGoogle,
		models.AutopilotProviderAnthropic,
	} {
		t.Run(string(prov), func(t *testing.T) {
			s, ct := newServiceForTest(&models.AutopilotSettings{
				Enabled:  true,
				Provider: prov,
				APIKey:   "ignored",
			})

			if _, err := s.Analyze("msg"); err != nil {
				t.Fatalf("Analyze: %v", err)
			}

			host := ct.lastReq.URL.Host
			if !strings.Contains(host, "api.anthropic.com") {
				t.Errorf("provider=%s routed to host %q, want api.anthropic.com", prov, host)
			}
		})
	}
}
