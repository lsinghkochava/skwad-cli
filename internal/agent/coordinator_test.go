package agent

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/models"
	"github.com/lsinghkochava/skwad-cli/internal/persistence"
)

// newTestCoordinator creates a Coordinator backed by a temp store for testing.
func newTestCoordinator(t *testing.T) *Coordinator {
	t.Helper()
	dir := t.TempDir()
	store, err := persistence.NewStoreAt(dir)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	mgr, err := NewManager(store)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return NewCoordinator(mgr)
}

// registerAgent adds an agent to manager and registers it with the coordinator.
func registerAgent(t *testing.T, c *Coordinator, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	a := &models.Agent{
		ID:        id,
		Name:      name,
		AgentType: models.AgentTypeClaude,
		Folder:    "/tmp",
		Status:    models.AgentStatusIdle,
	}
	c.manager.AddAgent(a, nil)
	c.RegisterAgent(id, name, "/tmp", "")
	return id
}

// injectMessage directly adds an unread message to an agent's inbox without
// spawning async goroutines. This avoids the race condition that SendMessage
// introduces via `go func() { c.NotifyIdleAgent(id) }()`.
func injectMessage(t *testing.T, c *Coordinator, fromID uuid.UUID, fromName string, toID uuid.UUID, content string) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()

	ra, ok := c.registered[toID]
	if !ok {
		t.Fatalf("agent %v not registered", toID)
	}
	ra.inbox = append(ra.inbox, Message{
		ID:        uuid.New(),
		FromID:    fromID,
		FromName:  fromName,
		ToID:      toID,
		Content:   content,
		Timestamp: time.Now(),
	})
}

// --- Regression test: message replay bug ---

func TestNotifyIdleAgent_NoReplay(t *testing.T) {
	c := newTestCoordinator(t)

	senderID := registerAgent(t, c, "Coder")
	receiverID := registerAgent(t, c, "Tester")

	// Inject a message directly (no async goroutine).
	injectMessage(t, c, senderID, "Coder", receiverID, "fix the tests")

	var deliverCount atomic.Int32
	c.OnDeliverMessage = func(agentID uuid.UUID, text string) {
		deliverCount.Add(1)
	}

	// First NotifyIdleAgent should deliver the message.
	c.NotifyIdleAgent(receiverID)
	if deliverCount.Load() != 1 {
		t.Errorf("first NotifyIdleAgent: deliverCount = %d, want 1", deliverCount.Load())
	}

	// Second NotifyIdleAgent should NOT deliver again — message is already read.
	c.NotifyIdleAgent(receiverID)
	if deliverCount.Load() != 1 {
		t.Errorf("second NotifyIdleAgent: deliverCount = %d, want 1 (no replay)", deliverCount.Load())
	}

	// Third call for good measure.
	c.NotifyIdleAgent(receiverID)
	if deliverCount.Load() != 1 {
		t.Errorf("third NotifyIdleAgent: deliverCount = %d, want 1 (no replay)", deliverCount.Load())
	}
}

// --- Happy path ---

func TestNotifyIdleAgent_DeliversUnreadMessage(t *testing.T) {
	c := newTestCoordinator(t)

	senderID := registerAgent(t, c, "Coder")
	receiverID := registerAgent(t, c, "Tester")

	injectMessage(t, c, senderID, "Coder", receiverID, "hello tester")

	var deliveredText string
	var deliveredTo uuid.UUID
	c.OnDeliverMessage = func(agentID uuid.UUID, text string) {
		deliveredTo = agentID
		deliveredText = text
	}

	c.NotifyIdleAgent(receiverID)

	if deliveredTo != receiverID {
		t.Errorf("delivered to %v, want %v", deliveredTo, receiverID)
	}
	if deliveredText == "" {
		t.Error("deliveredText should not be empty")
	}
}

func TestNotifyIdleAgent_NoCallback_NoPanic(t *testing.T) {
	c := newTestCoordinator(t)

	senderID := registerAgent(t, c, "Coder")
	receiverID := registerAgent(t, c, "Tester")

	injectMessage(t, c, senderID, "Coder", receiverID, "hello")

	// No OnDeliverMessage set — should not panic.
	c.OnDeliverMessage = nil
	c.NotifyIdleAgent(receiverID)

	// Message should still be marked as read.
	msgs := c.CheckMessages(receiverID, false)
	unread := 0
	for _, m := range msgs {
		if !m.Read {
			unread++
		}
	}
	if unread != 0 {
		t.Errorf("unread = %d, want 0 (message marked read even without callback)", unread)
	}
}

func TestNotifyIdleAgent_UnregisteredAgent_NoPanic(t *testing.T) {
	c := newTestCoordinator(t)

	// Call with an ID that was never registered — should not panic.
	c.NotifyIdleAgent(uuid.New())
}

// --- Multiple messages queued ---

func TestNotifyIdleAgent_MultipleMessages_OnePerCall(t *testing.T) {
	c := newTestCoordinator(t)

	senderID := registerAgent(t, c, "Coder")
	receiverID := registerAgent(t, c, "Tester")

	// Inject 3 messages directly.
	for _, msg := range []string{"msg1", "msg2", "msg3"} {
		injectMessage(t, c, senderID, "Coder", receiverID, msg)
	}

	var deliverCount atomic.Int32
	c.OnDeliverMessage = func(agentID uuid.UUID, text string) {
		deliverCount.Add(1)
	}

	// First notify delivers msg1.
	c.NotifyIdleAgent(receiverID)
	if deliverCount.Load() != 1 {
		t.Errorf("after 1st notify: deliverCount = %d, want 1", deliverCount.Load())
	}

	// Second notify delivers msg2.
	c.NotifyIdleAgent(receiverID)
	if deliverCount.Load() != 2 {
		t.Errorf("after 2nd notify: deliverCount = %d, want 2", deliverCount.Load())
	}

	// Third notify delivers msg3.
	c.NotifyIdleAgent(receiverID)
	if deliverCount.Load() != 3 {
		t.Errorf("after 3rd notify: deliverCount = %d, want 3", deliverCount.Load())
	}

	// Fourth notify — no more unread messages, should not deliver.
	c.NotifyIdleAgent(receiverID)
	if deliverCount.Load() != 3 {
		t.Errorf("after 4th notify: deliverCount = %d, want 3 (no more unread)", deliverCount.Load())
	}
}

// --- Broadcast + NotifyIdleAgent ---

func TestBroadcast_NotifyIdleAgent_MarksRead(t *testing.T) {
	c := newTestCoordinator(t)

	senderID := registerAgent(t, c, "Manager")
	receiverID := registerAgent(t, c, "Coder")

	// Inject a broadcast message directly.
	injectMessage(t, c, senderID, "Manager", receiverID, "all hands meeting")

	var deliverCount atomic.Int32
	c.OnDeliverMessage = func(agentID uuid.UUID, text string) {
		deliverCount.Add(1)
	}

	// NotifyIdleAgent should deliver.
	c.NotifyIdleAgent(receiverID)
	if deliverCount.Load() != 1 {
		t.Errorf("after notify: deliverCount = %d, want 1", deliverCount.Load())
	}

	// Second notify should NOT re-deliver.
	c.NotifyIdleAgent(receiverID)
	if deliverCount.Load() != 1 {
		t.Errorf("after second notify: deliverCount = %d, want 1 (no replay)", deliverCount.Load())
	}
}

// --- Mixed read/unread with CheckMessages ---

func TestNotifyIdleAgent_SkipsAlreadyReadMessages(t *testing.T) {
	c := newTestCoordinator(t)

	senderID := registerAgent(t, c, "Coder")
	receiverID := registerAgent(t, c, "Tester")

	// Inject 3 messages directly.
	for _, msg := range []string{"msg1", "msg2", "msg3"} {
		injectMessage(t, c, senderID, "Coder", receiverID, msg)
	}

	// Mark all as read via CheckMessages.
	c.CheckMessages(receiverID, true)

	var deliveredMessages []string
	c.OnDeliverMessage = func(agentID uuid.UUID, text string) {
		deliveredMessages = append(deliveredMessages, text)
	}

	// NotifyIdleAgent should NOT deliver anything — all already read.
	c.NotifyIdleAgent(receiverID)
	if len(deliveredMessages) != 0 {
		t.Errorf("should not deliver already-read messages, got %d deliveries", len(deliveredMessages))
	}

	// Inject a new unread message.
	injectMessage(t, c, senderID, "Coder", receiverID, "msg4")

	// Now NotifyIdleAgent should deliver only msg4.
	c.NotifyIdleAgent(receiverID)
	if len(deliveredMessages) != 1 {
		t.Fatalf("should deliver 1 new message, got %d", len(deliveredMessages))
	}
	if deliveredMessages[0] == "" {
		t.Error("delivered text should not be empty")
	}

	// And calling again should not re-deliver.
	c.NotifyIdleAgent(receiverID)
	if len(deliveredMessages) != 1 {
		t.Errorf("should still be 1 delivery after second notify, got %d", len(deliveredMessages))
	}
}

// --- SendMessage integration ---

func TestSendMessage_TargetNotFound(t *testing.T) {
	c := newTestCoordinator(t)

	senderID := registerAgent(t, c, "Coder")

	err := c.SendMessage(senderID, "nonexistent", "hello")
	if err != ErrAgentNotFound {
		t.Errorf("SendMessage to unknown agent: err = %v, want ErrAgentNotFound", err)
	}
}

func TestSendMessage_ByName(t *testing.T) {
	c := newTestCoordinator(t)

	senderID := registerAgent(t, c, "Coder")
	receiverID := registerAgent(t, c, "Tester")

	err := c.SendMessage(senderID, "Tester", "hello by name")
	if err != nil {
		t.Fatalf("SendMessage by name: %v", err)
	}

	// Verify message landed in inbox (may already be read from async notify).
	msgs := c.CheckMessages(receiverID, false)
	if len(msgs) == 0 {
		t.Error("expected at least 1 message in inbox")
	}
}

func TestSendMessage_QueuesAndNotifiesAsync(t *testing.T) {
	c := newTestCoordinator(t)

	senderID := registerAgent(t, c, "Coder")
	receiverID := registerAgent(t, c, "Tester")

	// Track async delivery.
	var delivered sync.WaitGroup
	delivered.Add(1)
	c.OnDeliverMessage = func(agentID uuid.UUID, text string) {
		delivered.Done()
	}

	err := c.SendMessage(senderID, receiverID.String(), "async delivery test")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	// Wait for async goroutine to deliver (with timeout).
	done := make(chan struct{})
	go func() {
		delivered.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Async delivery worked.
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for async delivery")
	}
}

// --- CheckMessages ---

func TestCheckMessages_MarkReadFalse_DoesNotMarkRead(t *testing.T) {
	c := newTestCoordinator(t)

	senderID := registerAgent(t, c, "Coder")
	receiverID := registerAgent(t, c, "Tester")

	// Inject directly to avoid async race.
	injectMessage(t, c, senderID, "Coder", receiverID, "hello")

	// Check without marking read.
	msgs := c.CheckMessages(receiverID, false)
	if len(msgs) == 0 {
		t.Fatal("expected at least 1 message")
	}

	// Check again — should still have unread.
	msgs = c.CheckMessages(receiverID, false)
	unread := 0
	for _, m := range msgs {
		if !m.Read {
			unread++
		}
	}
	if unread == 0 {
		t.Error("messages should still be unread when markRead=false")
	}
}

func TestCheckMessages_UnregisteredAgent(t *testing.T) {
	c := newTestCoordinator(t)

	msgs := c.CheckMessages(uuid.New(), true)
	if msgs != nil {
		t.Errorf("CheckMessages for unregistered agent should return nil, got %v", msgs)
	}
}
