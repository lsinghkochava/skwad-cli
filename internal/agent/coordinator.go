package agent

import (
	"fmt"
	"log/slog"
	"sort"
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

	// Task management
	tasks    map[uuid.UUID]*models.Task
	maxTasks int

	// OnDeliverMessage is called when a message should be injected into a terminal.
	// Returns an error if delivery failed; the message will remain unread.
	OnDeliverMessage func(agentID uuid.UUID, text string) error

	// OnMessageSent is called after a message is successfully queued.
	OnMessageSent func(fromID, fromName, toID, content string)
	// OnBroadcast is called after a broadcast message is queued.
	OnBroadcast func(fromID, fromName, content string)
	// OnStatusChanged is called after an agent's status text is updated.
	OnStatusChanged func(agentID, agentName, status, category string)

	// OnAgentIdle is fired asynchronously via goroutine when an agent goes idle
	// and has no unread messages. Ordering is best-effort, not guaranteed.
	OnAgentIdle func(agentID uuid.UUID)
	// OnTaskCreated is fired asynchronously via goroutine after a new task is
	// created. Ordering is best-effort, not guaranteed.
	OnTaskCreated func(task *models.Task)
	// OnTaskCompleted is fired asynchronously via goroutine after a task is
	// marked completed. Ordering is best-effort, not guaranteed.
	OnTaskCompleted func(task *models.Task)
}

type registeredAgent struct {
	info  AgentInfo
	inbox []Message
}

// NewCoordinator creates a Coordinator backed by the given Manager.
func NewCoordinator(mgr *Manager) *Coordinator {
	return &Coordinator{
		manager:    mgr,
		registered: make(map[uuid.UUID]*registeredAgent),
		tasks:      make(map[uuid.UUID]*models.Task),
		maxTasks:   50,
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

// Persona returns a persona by ID from the underlying manager.
func (c *Coordinator) Persona(id uuid.UUID) *models.Persona {
	return c.manager.Persona(id)
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

	if c.OnMessageSent != nil {
		c.OnMessageSent(fromID.String(), fromName, targetID.String(), content)
	}

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

	if c.OnBroadcast != nil {
		c.OnBroadcast(fromID.String(), fromName, content)
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
		if !ra.inbox[i].Read {
			if c.OnDeliverMessage != nil {
				text := buildNotificationText(ra.inbox[i])
				if err := c.OnDeliverMessage(agentID, text); err != nil {
					slog.Debug("message delivery failed, keeping unread", "agentID", agentID, "error", err)
					return
				}
			}
			ra.inbox[i].Read = true
			return
		}
	}

	// No unread messages — fire idle callback in a goroutine to avoid deadlock.
	if c.OnAgentIdle != nil {
		cb := c.OnAgentIdle
		go cb(agentID)
	}
}

// SetStatusText sets a human-readable status text and category on an agent.
func (c *Coordinator) SetStatusText(agentID uuid.UUID, status, category string) {
	c.manager.UpdateAgent(agentID, func(a *models.Agent) {
		a.StatusText = status
		a.StatusCategory = category
	})
	if c.OnStatusChanged != nil {
		agentName := ""
		if a, ok := c.manager.Agent(agentID); ok {
			agentName = a.Name
		}
		c.OnStatusChanged(agentID.String(), agentName, status, category)
	}
}

// HasUnreadMessages returns true if any registered agent has unread inbox messages.
func (c *Coordinator) HasUnreadMessages() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, entry := range c.registered {
		for _, msg := range entry.inbox {
			if !msg.Read {
				return true
			}
		}
	}
	return false
}

// UnregisterAgent removes an agent from the registry (e.g., on close).
func (c *Coordinator) UnregisterAgent(id uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.registered, id)
}

// ---------------------------------------------------------------------------
// Task management
// ---------------------------------------------------------------------------

// CreateTask creates a new task, validates dependencies, and checks for cycles.
// When selfAssign is true, the task is atomically assigned to the creator and
// set to in-progress (or blocked if dependencies are incomplete).
func (c *Coordinator) CreateTask(createdBy uuid.UUID, title, description string, deps []uuid.UUID, selfAssign bool, preferredRole string, tags []string) (*models.Task, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.tasks) >= c.maxTasks {
		return nil, fmt.Errorf("task limit reached (%d)", c.maxTasks)
	}

	for _, depID := range deps {
		if _, ok := c.tasks[depID]; !ok {
			return nil, fmt.Errorf("dependency task %s not found", depID)
		}
	}

	taskID := uuid.New()
	if c.hasCircularDep(taskID, deps) {
		return nil, fmt.Errorf("circular dependency detected")
	}

	status := models.TaskStatusPending
	for _, depID := range deps {
		if dep := c.tasks[depID]; dep.Status != models.TaskStatusCompleted {
			status = models.TaskStatusBlocked
			break
		}
	}

	task := &models.Task{
		ID:            taskID,
		Title:         title,
		Description:   description,
		Status:        status,
		PreferredRole: preferredRole,
		Tags:          tags,
		CreatedBy:     createdBy,
		Dependencies:  deps,
		CreatedAt:     time.Now(),
	}

	if selfAssign {
		task.AssigneeID = &createdBy
		if ra, ok := c.registered[createdBy]; ok {
			task.AssigneeName = ra.info.Name
		}
		if task.Status != models.TaskStatusBlocked {
			task.Status = models.TaskStatusInProgress
		}
	}

	c.tasks[taskID] = task

	if c.OnTaskCreated != nil {
		cb := c.OnTaskCreated
		taskCopy := *task
		go cb(&taskCopy)
	}

	return task, nil
}

// hasCircularDep uses DFS with a recursion stack to detect cycles. It catches
// both cycles back to the new taskID and pre-existing cycles in the dependency
// graph. Diamond DAGs are handled correctly: a node visited via one path is
// skipped via the visited set without triggering a false positive, since only
// nodes currently on the recursion stack indicate a real cycle.
func (c *Coordinator) hasCircularDep(taskID uuid.UUID, deps []uuid.UUID) bool {
	visited := make(map[uuid.UUID]bool)
	onStack := make(map[uuid.UUID]bool)

	var dfs func(id uuid.UUID) bool
	dfs = func(id uuid.UUID) bool {
		if id == taskID {
			return true
		}
		if onStack[id] {
			return true
		}
		if visited[id] {
			return false
		}
		visited[id] = true
		onStack[id] = true

		if t, ok := c.tasks[id]; ok && t.Status != models.TaskStatusCompleted {
			for _, dep := range t.Dependencies {
				if dfs(dep) {
					return true
				}
			}
		}

		onStack[id] = false
		return false
	}

	for _, dep := range deps {
		if dfs(dep) {
			return true
		}
	}
	return false
}

// ListTasks returns all tasks sorted by CreatedAt ascending (FIFO).
func (c *Coordinator) ListTasks() []*models.Task {
	c.mu.Lock()
	defer c.mu.Unlock()

	list := make([]*models.Task, 0, len(c.tasks))
	for _, t := range c.tasks {
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.Before(list[j].CreatedAt)
	})
	return list
}

// GetTask returns a task by ID.
func (c *Coordinator) GetTask(taskID uuid.UUID) (*models.Task, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	t, ok := c.tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("task %s not found", taskID)
	}
	return t, nil
}

// ClaimTask assigns a pending task to a registered agent.
func (c *Coordinator) ClaimTask(agentID uuid.UUID, taskID uuid.UUID) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	ra, ok := c.registered[agentID]
	if !ok {
		return fmt.Errorf("agent %s is not registered", agentID)
	}

	t, ok := c.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}
	if t.Status != models.TaskStatusPending {
		return fmt.Errorf("task %s is not pending (status: %s)", taskID, t.Status)
	}
	if t.AssigneeID != nil {
		return fmt.Errorf("task %s is already assigned", taskID)
	}

	t.AssigneeID = &agentID
	t.AssigneeName = ra.info.Name
	t.Status = models.TaskStatusInProgress
	return nil
}

// CompleteTask marks a task as completed and auto-unblocks dependent tasks.
func (c *Coordinator) CompleteTask(agentID uuid.UUID, taskID uuid.UUID) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	t, ok := c.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}
	if t.AssigneeID == nil || *t.AssigneeID != agentID {
		return fmt.Errorf("agent %s is not the assignee of task %s", agentID, taskID)
	}

	now := time.Now()
	t.Status = models.TaskStatusCompleted
	t.CompletedAt = &now

	// O(n) scan over all tasks — acceptable at maxTasks=50. Consider reverse index if maxTasks grows.
	for _, other := range c.tasks {
		if other.Status != models.TaskStatusBlocked {
			continue
		}
		remaining := make([]uuid.UUID, 0, len(other.Dependencies))
		for _, depID := range other.Dependencies {
			if depID != taskID {
				remaining = append(remaining, depID)
			}
		}
		other.Dependencies = remaining

		if len(other.Dependencies) == 0 {
			other.Status = models.TaskStatusPending
		}
	}

	if c.OnTaskCompleted != nil {
		cb := c.OnTaskCompleted
		taskCopy := *t
		go cb(&taskCopy)
	}

	return nil
}

// UpdateTask updates the title and/or description of a task. Only the creator
// or current assignee may update.
func (c *Coordinator) UpdateTask(callerID uuid.UUID, taskID uuid.UUID, title, description string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	t, ok := c.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}

	isCreator := callerID == t.CreatedBy
	isAssignee := t.AssigneeID != nil && *t.AssigneeID == callerID
	if !isCreator && !isAssignee {
		return fmt.Errorf("agent %s is not authorized to update task %s", callerID, taskID)
	}

	if title != "" {
		t.Title = title
	}
	if description != "" {
		t.Description = description
	}
	return nil
}

// SetMaxTasks sets the maximum number of tasks the coordinator will accept.
func (c *Coordinator) SetMaxTasks(max int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maxTasks = max
}

// LoadTasks bulk-loads tasks into the coordinator (e.g., for persistence restore).
// Blocked tasks are re-evaluated: if all dependencies are completed, they are
// promoted to Pending.
func (c *Coordinator) LoadTasks(tasks []*models.Task) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, t := range tasks {
		c.tasks[t.ID] = t
	}

	// Re-evaluate blocked tasks for consistency after load.
	for _, t := range c.tasks {
		if t.Status != models.TaskStatusBlocked {
			continue
		}
		allComplete := true
		for _, depID := range t.Dependencies {
			if dep, ok := c.tasks[depID]; !ok || dep.Status != models.TaskStatusCompleted {
				allComplete = false
				break
			}
		}
		if allComplete {
			t.Status = models.TaskStatusPending
		}
	}
}

func buildNotificationText(m Message) string {
	name := m.FromName
	if name == "" {
		name = "User (external)"
	}
	text := "[Skwad] Message from " + name + ":\n" + m.Content
	const maxNotificationLen = 100000
	if len(text) > maxNotificationLen {
		text = text[:maxNotificationLen] + "\n[truncated — run check-messages for full text]"
	}
	return text
}

// ErrAgentNotFound is returned when a target agent cannot be found.
var ErrAgentNotFound = fmt.Errorf("agent not found")
