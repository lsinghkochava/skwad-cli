package agent

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/models"
)

// Message is an in-memory inter-agent message.
type Message struct {
	ID        uuid.UUID
	FromID    uuid.UUID
	FromName  string
	ToID      uuid.UUID
	Content   string
	Timestamp time.Time
	Read      bool
}

// AgentInfo is the public view of an agent exposed to MCP callers.
type AgentInfo struct {
	ID           uuid.UUID
	Name         string
	Folder       string
	Status       models.AgentStatus
	IsRegistered bool
}

// Coordinator is a goroutine-safe message queue and agent registry for MCP.
// It is intentionally separate from Manager to avoid locking the UI thread.
type Coordinator struct {
	mu      sync.Mutex
	manager *Manager

	// registered holds agents that have called register-agent.
	registered map[uuid.UUID]*registeredAgent

	// OnDeliverMessage is called when a message should be injected into a terminal.
	OnDeliverMessage func(agentID uuid.UUID, text string)
}

type registeredAgent struct {
	info          AgentInfo
	inbox         []Message
	lastNotifiedID uuid.UUID
}

// NewCoordinator creates a Coordinator backed by the given Manager.
func NewCoordinator(mgr *Manager) *Coordinator {
	return &Coordinator{
		manager:    mgr,
		registered: make(map[uuid.UUID]*registeredAgent),
	}
}

// RegisterAgent registers an agent with the coordinator and returns the current
// member list plus the number of unread messages waiting in the inbox.
func (c *Coordinator) RegisterAgent(id uuid.UUID, name, folder string, sessionID string) ([]AgentInfo, int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ra, exists := c.registered[id]
	if !exists {
		ra = &registeredAgent{}
		c.registered[id] = ra
	}
	ra.info = AgentInfo{ID: id, Name: name, Folder: folder, IsRegistered: true}

	// Update session ID on the Manager side.
	c.manager.UpdateAgent(id, func(a *models.Agent) {
		a.IsRegistered = true
		if sessionID != "" {
			a.SessionID = sessionID
		}
	})

	unread := 0
	for _, m := range ra.inbox {
		if !m.Read {
			unread++
		}
	}
	return c.memberList(), unread, nil
}

// Agent returns an agent by ID from the underlying manager.
func (c *Coordinator) Agent(id uuid.UUID) (*models.Agent, bool) {
	return c.manager.Agent(id)
}

// AllAgents returns every agent from the underlying manager.
func (c *Coordinator) AllAgents() []*models.Agent {
	return c.manager.AllAgents()
}

// ListAgents returns all currently registered agents.
func (c *Coordinator) ListAgents() []AgentInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.memberList()
}

func (c *Coordinator) memberList() []AgentInfo {
	list := make([]AgentInfo, 0, len(c.registered))
	for _, ra := range c.registered {
		// Refresh status from Manager.
		if a, ok := c.manager.Agent(ra.info.ID); ok {
			ra.info.Status = a.Status
			ra.info.IsRegistered = a.IsRegistered
		}
		list = append(list, ra.info)
	}
	return list
}

// SendMessage delivers a message from one agent to another by ID or name.
func (c *Coordinator) SendMessage(fromID uuid.UUID, toIDOrName, content string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var target *registeredAgent
	for _, ra := range c.registered {
		if ra.info.ID.String() == toIDOrName || ra.info.Name == toIDOrName {
			target = ra
			break
		}
	}
	if target == nil {
		return ErrAgentNotFound
	}

	fromName := ""
	if ra, ok := c.registered[fromID]; ok {
		fromName = ra.info.Name
	}

	msg := Message{
		ID:        uuid.New(),
		FromID:    fromID,
		FromName:  fromName,
		ToID:      target.info.ID,
		Content:   content,
		Timestamp: time.Now(),
	}
	target.inbox = append(target.inbox, msg)

	// Notify immediately if the agent is already idle.
	targetID := target.info.ID
	go func() { c.NotifyIdleAgent(targetID) }()

	return nil
}

// BroadcastMessage sends a message from one agent to all other registered agents.
func (c *Coordinator) BroadcastMessage(fromID uuid.UUID, content string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	fromName := ""
	if ra, ok := c.registered[fromID]; ok {
		fromName = ra.info.Name
	}

	for id, ra := range c.registered {
		if id == fromID {
			continue
		}
		msg := Message{
			ID:        uuid.New(),
			FromID:    fromID,
			FromName:  fromName,
			ToID:      id,
			Content:   content,
			Timestamp: time.Now(),
		}
		ra.inbox = append(ra.inbox, msg)
	}

	// Notify all recipients immediately.
	for id := range c.registered {
		if id == fromID {
			continue
		}
		rid := id
		go func() { c.NotifyIdleAgent(rid) }()
	}
}

// CheckMessages returns the inbox for an agent, optionally marking messages as read.
func (c *Coordinator) CheckMessages(agentID uuid.UUID, markRead bool) []Message {
	c.mu.Lock()
	defer c.mu.Unlock()

	ra, ok := c.registered[agentID]
	if !ok {
		return nil
	}

	result := make([]Message, len(ra.inbox))
	copy(result, ra.inbox)

	if markRead {
		for i := range ra.inbox {
			ra.inbox[i].Read = true
		}
	}
	return result
}

// NotifyIdleAgent checks for unread messages for an agent that just went idle
// and delivers them via the OnDeliverMessage callback.
func (c *Coordinator) NotifyIdleAgent(agentID uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ra, ok := c.registered[agentID]
	if !ok {
		return
	}

	for i := range ra.inbox {
		msg := &ra.inbox[i]
		if !msg.Read && msg.ID != ra.lastNotifiedID {
			ra.lastNotifiedID = msg.ID
			if c.OnDeliverMessage != nil {
				text := buildNotificationText(*msg)
				c.mu.Unlock()
				c.OnDeliverMessage(agentID, text)
				c.mu.Lock()
			}
			return
		}
	}
}

// SetStatusText sets a human-readable status text and category on an agent.
func (c *Coordinator) SetStatusText(agentID uuid.UUID, status, category string) {
	c.manager.UpdateAgent(agentID, func(a *models.Agent) {
		a.StatusText = status
		a.StatusCategory = category
	})
}

// UnregisterAgent removes an agent from the registry (e.g., on close).
func (c *Coordinator) UnregisterAgent(id uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.registered, id)
}

func buildNotificationText(m Message) string {
	text := "[Skwad] Message from " + m.FromName + ":\n" + m.Content
	const maxNotificationLen = 2000
	if len(text) > maxNotificationLen {
		text = text[:maxNotificationLen] + "\n[truncated — run check-messages for full text]"
	}
	return text
}

// ErrAgentNotFound is returned when a target agent cannot be found.
var ErrAgentNotFound = fmt.Errorf("agent not found")
