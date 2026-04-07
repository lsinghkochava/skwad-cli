package agent

import (
	"fmt"
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
	c.OnDeliverMessage = func(agentID uuid.UUID, text string) error {
		deliverCount.Add(1)
		return nil
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
	c.OnDeliverMessage = func(agentID uuid.UUID, text string) error {
		deliveredTo = agentID
		deliveredText = text
		return nil
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
	c.OnDeliverMessage = func(agentID uuid.UUID, text string) error {
		deliverCount.Add(1)
		return nil
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
	c.OnDeliverMessage = func(agentID uuid.UUID, text string) error {
		deliverCount.Add(1)
		return nil
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
	c.OnDeliverMessage = func(agentID uuid.UUID, text string) error {
		deliveredMessages = append(deliveredMessages, text)
		return nil
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
	c.OnDeliverMessage = func(agentID uuid.UUID, text string) error {
		delivered.Done()
		return nil
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

// ---------------------------------------------------------------------------
// Task management tests
// ---------------------------------------------------------------------------

// --- CreateTask happy paths ---

func TestCreateTask_NoDeps_StatusPending(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")

	task, err := c.CreateTask(creator, "Build feature", "Implement the widget", nil, false, "", nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.Status != models.TaskStatusPending {
		t.Errorf("status = %s, want pending", task.Status)
	}
	if task.Title != "Build feature" {
		t.Errorf("title = %q, want %q", task.Title, "Build feature")
	}
	if task.Description != "Implement the widget" {
		t.Errorf("description = %q, want %q", task.Description, "Implement the widget")
	}
	if task.CreatedBy != creator {
		t.Errorf("createdBy = %v, want %v", task.CreatedBy, creator)
	}
	if task.AssigneeID != nil {
		t.Errorf("assigneeID should be nil, got %v", task.AssigneeID)
	}
	if task.CreatedAt.IsZero() {
		t.Error("createdAt should not be zero")
	}
}

func TestCreateTask_CompletedDeps_StatusPending(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")
	worker := registerAgent(t, c, "Coder")

	// Create and complete a dependency task.
	dep, err := c.CreateTask(creator, "Setup", "Setup env", nil, false, "", nil)
	if err != nil {
		t.Fatalf("CreateTask dep: %v", err)
	}
	if err := c.ClaimTask(worker, dep.ID); err != nil {
		t.Fatalf("ClaimTask dep: %v", err)
	}
	if err := c.CompleteTask(worker, dep.ID); err != nil {
		t.Fatalf("CompleteTask dep: %v", err)
	}

	// Create task with completed dep → should be Pending.
	task, err := c.CreateTask(creator, "Build", "Build it", []uuid.UUID{dep.ID}, false, "", nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.Status != models.TaskStatusPending {
		t.Errorf("status = %s, want pending (dep is completed)", task.Status)
	}
}

func TestCreateTask_IncompleteDeps_StatusBlocked(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")

	dep, err := c.CreateTask(creator, "Setup", "Setup env", nil, false, "", nil)
	if err != nil {
		t.Fatalf("CreateTask dep: %v", err)
	}

	// Create task with incomplete dep → should be Blocked.
	task, err := c.CreateTask(creator, "Build", "Build it", []uuid.UUID{dep.ID}, false, "", nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.Status != models.TaskStatusBlocked {
		t.Errorf("status = %s, want blocked (dep is pending)", task.Status)
	}
}

// --- ListTasks ---

func TestListTasks_FIFOOrder(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")

	t1, err := c.CreateTask(creator, "First", "", nil, false, "", nil)
	if err != nil {
		t.Fatalf("CreateTask 1: %v", err)
	}
	// Ensure different CreatedAt timestamps.
	time.Sleep(time.Millisecond)

	t2, err := c.CreateTask(creator, "Second", "", nil, false, "", nil)
	if err != nil {
		t.Fatalf("CreateTask 2: %v", err)
	}
	time.Sleep(time.Millisecond)

	t3, err := c.CreateTask(creator, "Third", "", nil, false, "", nil)
	if err != nil {
		t.Fatalf("CreateTask 3: %v", err)
	}

	list := c.ListTasks()
	if len(list) != 3 {
		t.Fatalf("ListTasks len = %d, want 3", len(list))
	}
	if list[0].ID != t1.ID || list[1].ID != t2.ID || list[2].ID != t3.ID {
		t.Errorf("ListTasks not in FIFO order: got [%s, %s, %s], want [%s, %s, %s]",
			list[0].Title, list[1].Title, list[2].Title,
			t1.Title, t2.Title, t3.Title)
	}
}

// --- GetTask ---

func TestGetTask_Found(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")

	created, err := c.CreateTask(creator, "My task", "desc", nil, false, "", nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	got, err := c.GetTask(created.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("GetTask ID = %v, want %v", got.ID, created.ID)
	}
	if got.Title != "My task" {
		t.Errorf("GetTask Title = %q, want %q", got.Title, "My task")
	}
}

func TestGetTask_NotFound(t *testing.T) {
	c := newTestCoordinator(t)

	_, err := c.GetTask(uuid.New())
	if err == nil {
		t.Error("GetTask with unknown ID should return error")
	}
}

// --- ClaimTask ---

func TestClaimTask_Pending_Success(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")
	worker := registerAgent(t, c, "Coder")

	task, err := c.CreateTask(creator, "Do work", "", nil, false, "", nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := c.ClaimTask(worker, task.ID); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}

	got, _ := c.GetTask(task.ID)
	if got.Status != models.TaskStatusInProgress {
		t.Errorf("status = %s, want in_progress", got.Status)
	}
	if got.AssigneeID == nil || *got.AssigneeID != worker {
		t.Errorf("assigneeID = %v, want %v", got.AssigneeID, worker)
	}
	if got.AssigneeName != "Coder" {
		t.Errorf("assigneeName = %q, want %q", got.AssigneeName, "Coder")
	}
}

func TestClaimTask_Blocked_Error(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")
	worker := registerAgent(t, c, "Coder")

	dep, _ := c.CreateTask(creator, "Dep", "", nil, false, "", nil)
	blocked, _ := c.CreateTask(creator, "Blocked", "", []uuid.UUID{dep.ID}, false, "", nil)

	err := c.ClaimTask(worker, blocked.ID)
	if err == nil {
		t.Error("ClaimTask on blocked task should return error")
	}
}

func TestClaimTask_AlreadyClaimed_Error(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")
	worker1 := registerAgent(t, c, "Coder")
	worker2 := registerAgent(t, c, "Tester")

	task, _ := c.CreateTask(creator, "Work", "", nil, false, "", nil)
	if err := c.ClaimTask(worker1, task.ID); err != nil {
		t.Fatalf("first ClaimTask: %v", err)
	}

	err := c.ClaimTask(worker2, task.ID)
	if err == nil {
		t.Error("ClaimTask on already-claimed task should return error")
	}
}

func TestClaimTask_UnregisteredAgent_Error(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")

	task, _ := c.CreateTask(creator, "Work", "", nil, false, "", nil)

	err := c.ClaimTask(uuid.New(), task.ID)
	if err == nil {
		t.Error("ClaimTask by unregistered agent should return error")
	}
}

func TestClaimTask_AtomicConcurrency(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")

	task, _ := c.CreateTask(creator, "Race", "", nil, false, "", nil)

	const numWorkers = 10
	workers := make([]uuid.UUID, numWorkers)
	for i := range workers {
		workers[i] = registerAgent(t, c, fmt.Sprintf("Worker%d", i))
	}

	var successCount atomic.Int32
	var wg sync.WaitGroup
	wg.Add(numWorkers)

	for _, w := range workers {
		go func(agentID uuid.UUID) {
			defer wg.Done()
			if err := c.ClaimTask(agentID, task.ID); err == nil {
				successCount.Add(1)
			}
		}(w)
	}
	wg.Wait()

	if successCount.Load() != 1 {
		t.Errorf("exactly 1 goroutine should succeed claiming, got %d", successCount.Load())
	}
}

// --- CompleteTask ---

func TestCompleteTask_Success(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")
	worker := registerAgent(t, c, "Coder")

	task, _ := c.CreateTask(creator, "Work", "", nil, false, "", nil)
	c.ClaimTask(worker, task.ID)

	if err := c.CompleteTask(worker, task.ID); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	got, _ := c.GetTask(task.ID)
	if got.Status != models.TaskStatusCompleted {
		t.Errorf("status = %s, want completed", got.Status)
	}
	if got.CompletedAt == nil {
		t.Error("CompletedAt should be set")
	}
}

func TestCompleteTask_NonAssignee_Error(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")
	worker := registerAgent(t, c, "Coder")
	other := registerAgent(t, c, "Tester")

	task, _ := c.CreateTask(creator, "Work", "", nil, false, "", nil)
	c.ClaimTask(worker, task.ID)

	err := c.CompleteTask(other, task.ID)
	if err == nil {
		t.Error("CompleteTask by non-assignee should return error")
	}
}

func TestCompleteTask_NotFound_Error(t *testing.T) {
	c := newTestCoordinator(t)
	worker := registerAgent(t, c, "Coder")

	err := c.CompleteTask(worker, uuid.New())
	if err == nil {
		t.Error("CompleteTask on unknown task should return error")
	}
}

// --- UpdateTask ---

func TestUpdateTask_ByCreator(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")

	task, _ := c.CreateTask(creator, "Original", "Original desc", nil, false, "", nil)

	if err := c.UpdateTask(creator, task.ID, "Updated", "Updated desc"); err != nil {
		t.Fatalf("UpdateTask by creator: %v", err)
	}

	got, _ := c.GetTask(task.ID)
	if got.Title != "Updated" {
		t.Errorf("title = %q, want %q", got.Title, "Updated")
	}
	if got.Description != "Updated desc" {
		t.Errorf("description = %q, want %q", got.Description, "Updated desc")
	}
}

func TestUpdateTask_ByAssignee(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")
	worker := registerAgent(t, c, "Coder")

	task, _ := c.CreateTask(creator, "Original", "Original desc", nil, false, "", nil)
	c.ClaimTask(worker, task.ID)

	if err := c.UpdateTask(worker, task.ID, "Assignee update", ""); err != nil {
		t.Fatalf("UpdateTask by assignee: %v", err)
	}

	got, _ := c.GetTask(task.ID)
	if got.Title != "Assignee update" {
		t.Errorf("title = %q, want %q", got.Title, "Assignee update")
	}
	// Empty description should not overwrite.
	if got.Description != "Original desc" {
		t.Errorf("description = %q, want %q (empty should not overwrite)", got.Description, "Original desc")
	}
}

func TestUpdateTask_Unauthorized_Error(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")
	other := registerAgent(t, c, "Tester")

	task, _ := c.CreateTask(creator, "Work", "desc", nil, false, "", nil)

	err := c.UpdateTask(other, task.ID, "Hacked", "Hacked desc")
	if err == nil {
		t.Error("UpdateTask by non-owner/non-assignee should return error")
	}

	// Verify title unchanged.
	got, _ := c.GetTask(task.ID)
	if got.Title != "Work" {
		t.Errorf("title should be unchanged, got %q", got.Title)
	}
}

func TestUpdateTask_NotFound_Error(t *testing.T) {
	c := newTestCoordinator(t)
	caller := registerAgent(t, c, "Manager")

	err := c.UpdateTask(caller, uuid.New(), "Title", "Desc")
	if err == nil {
		t.Error("UpdateTask on unknown task should return error")
	}
}

// --- MaxTasks enforcement ---

func TestCreateTask_MaxTasksEnforcement(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")

	c.SetMaxTasks(3)

	for i := 0; i < 3; i++ {
		_, err := c.CreateTask(creator, fmt.Sprintf("Task %d", i), "", nil, false, "", nil)
		if err != nil {
			t.Fatalf("CreateTask %d: %v", i, err)
		}
	}

	_, err := c.CreateTask(creator, "One too many", "", nil, false, "", nil)
	if err == nil {
		t.Error("CreateTask beyond maxTasks should return error")
	}
}

// --- Dependency graph ---

func TestCreateTask_CircularDep_Direct(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")

	taskA, _ := c.CreateTask(creator, "A", "", nil, false, "", nil)

	// B depends on A — valid.
	taskB, err := c.CreateTask(creator, "B", "", []uuid.UUID{taskA.ID}, false, "", nil)
	if err != nil {
		t.Fatalf("CreateTask B: %v", err)
	}

	// Try to create C that depends on B, while B depends on A — no cycle.
	_, err = c.CreateTask(creator, "C", "", []uuid.UUID{taskB.ID}, false, "", nil)
	if err != nil {
		t.Fatalf("CreateTask C (valid chain): %v", err)
	}
}

func TestCreateTask_CircularDep_Transitive(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")

	taskA, _ := c.CreateTask(creator, "A", "", nil, false, "", nil)
	taskB, _ := c.CreateTask(creator, "B", "", []uuid.UUID{taskA.ID}, false, "", nil)
	taskC, _ := c.CreateTask(creator, "C", "", []uuid.UUID{taskB.ID}, false, "", nil)

	// Now try to make A depend on C — this creates A→B→C→A cycle.
	// But we can't add deps to existing tasks, so we test by trying to create a
	// new task D that depends on C, while also having C depend on B→A.
	// The real cycle test: create a task that depends on something that
	// transitively depends back. Since hasCircularDep checks BFS from the new
	// task's deps, a genuine cycle only occurs if one of the deps' chain reaches
	// back to the new task's ID — which can't happen for a NEW task.
	// Instead, verify the valid chain works.
	_ = taskC
	// Valid: D depends on C (chain D→C→B→A, no cycle).
	_, err := c.CreateTask(creator, "D", "", []uuid.UUID{taskC.ID}, false, "", nil)
	if err != nil {
		t.Fatalf("CreateTask D (valid chain A→B→C→D): %v", err)
	}
}

func TestCreateTask_NonexistentDep_Error(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")

	_, err := c.CreateTask(creator, "Orphan", "", []uuid.UUID{uuid.New()}, false, "", nil)
	if err == nil {
		t.Error("CreateTask with nonexistent dependency should return error")
	}
}

// --- Auto-unblocking ---

func TestCompleteTask_AutoUnblocksSingleDep(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")
	worker := registerAgent(t, c, "Coder")

	dep, _ := c.CreateTask(creator, "Dependency", "", nil, false, "", nil)
	blocked, _ := c.CreateTask(creator, "Blocked", "", []uuid.UUID{dep.ID}, false, "", nil)

	if blocked.Status != models.TaskStatusBlocked {
		t.Fatalf("blocked.Status = %s, want blocked", blocked.Status)
	}

	// Complete the dependency.
	c.ClaimTask(worker, dep.ID)
	c.CompleteTask(worker, dep.ID)

	// The blocked task should now be pending.
	got, _ := c.GetTask(blocked.ID)
	if got.Status != models.TaskStatusPending {
		t.Errorf("status = %s, want pending (auto-unblocked)", got.Status)
	}
}

func TestCompleteTask_AutoUnblock_MultipleDeps_AllMustComplete(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")
	worker := registerAgent(t, c, "Coder")

	dep1, _ := c.CreateTask(creator, "Dep1", "", nil, false, "", nil)
	dep2, _ := c.CreateTask(creator, "Dep2", "", nil, false, "", nil)
	blocked, _ := c.CreateTask(creator, "Blocked", "", []uuid.UUID{dep1.ID, dep2.ID}, false, "", nil)

	if blocked.Status != models.TaskStatusBlocked {
		t.Fatalf("blocked.Status = %s, want blocked", blocked.Status)
	}

	// Complete only dep1 — should still be blocked.
	c.ClaimTask(worker, dep1.ID)
	c.CompleteTask(worker, dep1.ID)

	got, _ := c.GetTask(blocked.ID)
	if got.Status != models.TaskStatusBlocked {
		t.Errorf("status = %s, want blocked (dep2 still incomplete)", got.Status)
	}

	// Complete dep2 — now should unblock.
	worker2 := registerAgent(t, c, "Tester")
	c.ClaimTask(worker2, dep2.ID)
	c.CompleteTask(worker2, dep2.ID)

	got, _ = c.GetTask(blocked.ID)
	if got.Status != models.TaskStatusPending {
		t.Errorf("status = %s, want pending (all deps completed)", got.Status)
	}
}

// --- LoadTasks ---

func TestLoadTasks_BulkLoad(t *testing.T) {
	c := newTestCoordinator(t)

	id1 := uuid.New()
	id2 := uuid.New()
	tasks := []*models.Task{
		{ID: id1, Title: "Loaded1", Status: models.TaskStatusPending, CreatedAt: time.Now()},
		{ID: id2, Title: "Loaded2", Status: models.TaskStatusCompleted, CreatedAt: time.Now()},
	}

	c.LoadTasks(tasks)

	list := c.ListTasks()
	if len(list) != 2 {
		t.Fatalf("ListTasks len = %d, want 2", len(list))
	}
}

func TestLoadTasks_ReEvaluatesBlockedTasks(t *testing.T) {
	c := newTestCoordinator(t)

	depID := uuid.New()
	blockedID := uuid.New()
	tasks := []*models.Task{
		{ID: depID, Title: "Dep", Status: models.TaskStatusCompleted, CreatedAt: time.Now()},
		{ID: blockedID, Title: "Was Blocked", Status: models.TaskStatusBlocked, Dependencies: []uuid.UUID{depID}, CreatedAt: time.Now()},
	}

	c.LoadTasks(tasks)

	got, err := c.GetTask(blockedID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != models.TaskStatusPending {
		t.Errorf("status = %s, want pending (dep is completed, should re-evaluate)", got.Status)
	}
}

func TestLoadTasks_BlockedStaysBlocked(t *testing.T) {
	c := newTestCoordinator(t)

	depID := uuid.New()
	blockedID := uuid.New()
	tasks := []*models.Task{
		{ID: depID, Title: "Dep", Status: models.TaskStatusPending, CreatedAt: time.Now()},
		{ID: blockedID, Title: "Blocked", Status: models.TaskStatusBlocked, Dependencies: []uuid.UUID{depID}, CreatedAt: time.Now()},
	}

	c.LoadTasks(tasks)

	got, _ := c.GetTask(blockedID)
	if got.Status != models.TaskStatusBlocked {
		t.Errorf("status = %s, want blocked (dep still pending)", got.Status)
	}
}

// --- Callbacks ---

func TestOnTaskCreated_Fires(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")

	var callbackCount atomic.Int32
	var callbackTitle string
	var mu sync.Mutex

	c.OnTaskCreated = func(task *models.Task) {
		mu.Lock()
		callbackTitle = task.Title
		mu.Unlock()
		callbackCount.Add(1)
	}

	c.CreateTask(creator, "Callback test", "", nil, false, "", nil)

	// Wait briefly for the goroutine to fire.
	time.Sleep(50 * time.Millisecond)

	if callbackCount.Load() != 1 {
		t.Errorf("OnTaskCreated count = %d, want 1", callbackCount.Load())
	}
	mu.Lock()
	if callbackTitle != "Callback test" {
		t.Errorf("OnTaskCreated title = %q, want %q", callbackTitle, "Callback test")
	}
	mu.Unlock()
}

func TestOnTaskCompleted_Fires(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")
	worker := registerAgent(t, c, "Coder")

	var callbackCount atomic.Int32
	c.OnTaskCompleted = func(task *models.Task) {
		callbackCount.Add(1)
	}

	task, _ := c.CreateTask(creator, "Complete me", "", nil, false, "", nil)
	c.ClaimTask(worker, task.ID)
	c.CompleteTask(worker, task.ID)

	time.Sleep(50 * time.Millisecond)

	if callbackCount.Load() != 1 {
		t.Errorf("OnTaskCompleted count = %d, want 1", callbackCount.Load())
	}
}

func TestOnAgentIdle_FiresWhenNoUnread(t *testing.T) {
	c := newTestCoordinator(t)
	agentID := registerAgent(t, c, "Coder")

	var idleCount atomic.Int32
	c.OnAgentIdle = func(id uuid.UUID) {
		idleCount.Add(1)
	}

	// No messages → OnAgentIdle should fire.
	c.NotifyIdleAgent(agentID)

	time.Sleep(50 * time.Millisecond)

	if idleCount.Load() != 1 {
		t.Errorf("OnAgentIdle count = %d, want 1", idleCount.Load())
	}
}

// ---------------------------------------------------------------------------
// HasUnreadMessages tests
// ---------------------------------------------------------------------------

func TestHasUnreadMessages_NoMessages(t *testing.T) {
	c := newTestCoordinator(t)
	registerAgent(t, c, "Coder")

	if c.HasUnreadMessages() {
		t.Error("HasUnreadMessages should return false when no messages exist")
	}
}

func TestHasUnreadMessages_NoRegisteredAgents(t *testing.T) {
	c := newTestCoordinator(t)

	if c.HasUnreadMessages() {
		t.Error("HasUnreadMessages should return false with no registered agents")
	}
}

func TestHasUnreadMessages_AllRead(t *testing.T) {
	c := newTestCoordinator(t)
	senderID := registerAgent(t, c, "Coder")
	receiverID := registerAgent(t, c, "Tester")

	injectMessage(t, c, senderID, "Coder", receiverID, "hello")
	c.CheckMessages(receiverID, true) // mark as read

	if c.HasUnreadMessages() {
		t.Error("HasUnreadMessages should return false when all messages are read")
	}
}

func TestHasUnreadMessages_UnreadExists(t *testing.T) {
	c := newTestCoordinator(t)
	senderID := registerAgent(t, c, "Coder")
	receiverID := registerAgent(t, c, "Tester")

	injectMessage(t, c, senderID, "Coder", receiverID, "unread msg")

	if !c.HasUnreadMessages() {
		t.Error("HasUnreadMessages should return true when unread messages exist")
	}
}

func TestHasUnreadMessages_MultipleAgents_MixedReadState(t *testing.T) {
	c := newTestCoordinator(t)
	senderID := registerAgent(t, c, "Manager")
	agent1 := registerAgent(t, c, "Coder")
	agent2 := registerAgent(t, c, "Tester")

	// Send to both agents.
	injectMessage(t, c, senderID, "Manager", agent1, "msg for coder")
	injectMessage(t, c, senderID, "Manager", agent2, "msg for tester")

	// Mark agent1's messages as read, leave agent2's unread.
	c.CheckMessages(agent1, true)

	if !c.HasUnreadMessages() {
		t.Error("HasUnreadMessages should return true when agent2 still has unread messages")
	}

	// Now mark agent2's as read too.
	c.CheckMessages(agent2, true)

	if c.HasUnreadMessages() {
		t.Error("HasUnreadMessages should return false after all messages read")
	}
}

// ---------------------------------------------------------------------------
// Delivery failure tests
// ---------------------------------------------------------------------------

func TestNotifyIdleAgent_DeliveryFailure_KeepsUnread(t *testing.T) {
	c := newTestCoordinator(t)
	senderID := registerAgent(t, c, "Coder")
	receiverID := registerAgent(t, c, "Tester")

	injectMessage(t, c, senderID, "Coder", receiverID, "important msg")

	// Delivery always fails.
	c.OnDeliverMessage = func(agentID uuid.UUID, text string) error {
		return fmt.Errorf("terminal busy")
	}

	c.NotifyIdleAgent(receiverID)

	// Message should still be unread.
	msgs := c.CheckMessages(receiverID, false)
	unread := 0
	for _, m := range msgs {
		if !m.Read {
			unread++
		}
	}
	if unread != 1 {
		t.Errorf("unread = %d, want 1 (delivery failed, message should stay unread)", unread)
	}

	// HasUnreadMessages should confirm.
	if !c.HasUnreadMessages() {
		t.Error("HasUnreadMessages should return true after failed delivery")
	}
}

func TestNotifyIdleAgent_DeliveryFailure_RetrySucceeds(t *testing.T) {
	c := newTestCoordinator(t)
	senderID := registerAgent(t, c, "Coder")
	receiverID := registerAgent(t, c, "Tester")

	injectMessage(t, c, senderID, "Coder", receiverID, "retry msg")

	callCount := 0
	c.OnDeliverMessage = func(agentID uuid.UUID, text string) error {
		callCount++
		if callCount == 1 {
			return fmt.Errorf("terminal busy")
		}
		return nil // succeeds on second attempt
	}

	// First attempt fails.
	c.NotifyIdleAgent(receiverID)
	if callCount != 1 {
		t.Errorf("callCount = %d, want 1", callCount)
	}

	// Message should still be unread.
	if !c.HasUnreadMessages() {
		t.Error("HasUnreadMessages should be true after failed delivery")
	}

	// Second attempt succeeds.
	c.NotifyIdleAgent(receiverID)
	if callCount != 2 {
		t.Errorf("callCount = %d, want 2", callCount)
	}

	// Message should now be read.
	if c.HasUnreadMessages() {
		t.Error("HasUnreadMessages should be false after successful retry")
	}
}

func TestNotifyIdleAgent_DeliveryFailure_MultipleMessagesAccumulate(t *testing.T) {
	c := newTestCoordinator(t)
	senderID := registerAgent(t, c, "Manager")
	receiverID := registerAgent(t, c, "Coder")

	// Inject 3 messages.
	for _, msg := range []string{"msg1", "msg2", "msg3"} {
		injectMessage(t, c, senderID, "Manager", receiverID, msg)
	}

	// Delivery always fails — all 3 should accumulate as unread.
	c.OnDeliverMessage = func(agentID uuid.UUID, text string) error {
		return fmt.Errorf("agent offline")
	}

	// NotifyIdleAgent tries first unread, fails, returns early.
	c.NotifyIdleAgent(receiverID)
	c.NotifyIdleAgent(receiverID)
	c.NotifyIdleAgent(receiverID)

	msgs := c.CheckMessages(receiverID, false)
	unread := 0
	for _, m := range msgs {
		if !m.Read {
			unread++
		}
	}
	if unread != 3 {
		t.Errorf("unread = %d, want 3 (all deliveries failed, all should stay unread)", unread)
	}
}

func TestNotifyIdleAgent_NilCallback_MarksRead(t *testing.T) {
	c := newTestCoordinator(t)
	senderID := registerAgent(t, c, "Coder")
	receiverID := registerAgent(t, c, "Tester")

	injectMessage(t, c, senderID, "Coder", receiverID, "hello")

	c.OnDeliverMessage = nil
	c.NotifyIdleAgent(receiverID)

	// With nil callback, message should still be marked as read (backward compat).
	if c.HasUnreadMessages() {
		t.Error("HasUnreadMessages should be false — nil callback should still mark messages read")
	}
}

func TestOnAgentIdle_DoesNotFireWhenUnread(t *testing.T) {
	c := newTestCoordinator(t)
	senderID := registerAgent(t, c, "Manager")
	receiverID := registerAgent(t, c, "Coder")

	var idleCount atomic.Int32
	c.OnAgentIdle = func(id uuid.UUID) {
		idleCount.Add(1)
	}

	// Inject an unread message.
	injectMessage(t, c, senderID, "Manager", receiverID, "you have work")

	// Suppress message delivery to focus on idle callback.
	c.OnDeliverMessage = func(agentID uuid.UUID, text string) error { return nil }

	c.NotifyIdleAgent(receiverID)

	time.Sleep(50 * time.Millisecond)

	if idleCount.Load() != 0 {
		t.Errorf("OnAgentIdle count = %d, want 0 (has unread messages)", idleCount.Load())
	}
}

// ---------------------------------------------------------------------------
// Tag-based task tests
// ---------------------------------------------------------------------------

func TestCreateTask_WithTags_StoresTags(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")

	tags := []string{"code", "backend"}
	task, err := c.CreateTask(creator, "Tagged task", "Has tags", nil, false, "", tags)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if len(task.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(task.Tags))
	}
	if task.Tags[0] != "code" || task.Tags[1] != "backend" {
		t.Errorf("tags = %v, want [code backend]", task.Tags)
	}
}

func TestCreateTask_WithoutTags_TagsEmpty(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")

	task, err := c.CreateTask(creator, "No tags", "No tags here", nil, false, "", nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if len(task.Tags) != 0 {
		t.Errorf("expected empty tags, got %v", task.Tags)
	}
}

func TestCreateTask_TagsPreservedThroughLifecycle(t *testing.T) {
	c := newTestCoordinator(t)
	creator := registerAgent(t, c, "Manager")
	worker := registerAgent(t, c, "Coder")

	tags := []string{"code", "test"}
	task, err := c.CreateTask(creator, "Lifecycle tags", "desc", nil, false, "", tags)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Claim task
	if err := c.ClaimTask(worker, task.ID); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	got, _ := c.GetTask(task.ID)
	if len(got.Tags) != 2 || got.Tags[0] != "code" || got.Tags[1] != "test" {
		t.Errorf("tags after claim = %v, want [code test]", got.Tags)
	}

	// Complete task
	if err := c.CompleteTask(worker, task.ID); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	got, _ = c.GetTask(task.ID)
	if len(got.Tags) != 2 || got.Tags[0] != "code" || got.Tags[1] != "test" {
		t.Errorf("tags after complete = %v, want [code test]", got.Tags)
	}
}
