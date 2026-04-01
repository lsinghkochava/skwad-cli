package agent

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/models"
)

func newTestController() (*ActivityController, uuid.UUID) {
	id := uuid.New()
	// nil manager — setStatus skips manager.UpdateAgent when nil
	ac := NewActivityController(id, models.ActivityTrackingAll, nil)
	return ac, id
}

func TestOnStreamMessage_SystemInit_SetsRunning(t *testing.T) {
	ac, _ := newTestController()
	ac.OnStreamMessage("system", "init")

	ac.mu.Lock()
	defer ac.mu.Unlock()
	if ac.status != models.AgentStatusRunning {
		t.Errorf("expected Running, got %s", ac.status)
	}
}

func TestOnStreamMessage_Assistant_SetsRunning(t *testing.T) {
	ac, _ := newTestController()
	ac.OnStreamMessage("assistant", "")

	ac.mu.Lock()
	defer ac.mu.Unlock()
	if ac.status != models.AgentStatusRunning {
		t.Errorf("expected Running, got %s", ac.status)
	}
}

func TestOnStreamMessage_Result_SetsIdle(t *testing.T) {
	ac, _ := newTestController()
	// First set to running
	ac.OnStreamMessage("assistant", "")
	// Then result
	ac.OnStreamMessage("result", "success")

	ac.mu.Lock()
	defer ac.mu.Unlock()
	if ac.status != models.AgentStatusIdle {
		t.Errorf("expected Idle, got %s", ac.status)
	}
}

func TestOnStreamMessage_Result_FiresOnTurnComplete(t *testing.T) {
	ac, _ := newTestController()
	var called atomic.Bool
	ac.OnTurnComplete = func() {
		called.Store(true)
	}

	ac.OnStreamMessage("assistant", "")
	ac.OnStreamMessage("result", "success")

	if !called.Load() {
		t.Error("OnTurnComplete callback was not called")
	}
}

func TestOnStreamMessage_Result_NoCallbackNoPanic(t *testing.T) {
	ac, _ := newTestController()
	// OnTurnComplete is nil — should not panic
	ac.OnStreamMessage("assistant", "")
	ac.OnStreamMessage("result", "success")
}

func TestOnStreamMessage_UnknownType_NoChange(t *testing.T) {
	ac, _ := newTestController()
	ac.OnStreamMessage("unknown", "")

	ac.mu.Lock()
	defer ac.mu.Unlock()
	if ac.status != models.AgentStatusIdle {
		t.Errorf("unknown message type should not change status, got %s", ac.status)
	}
}

func TestOnProcessExit_Zero_SetsIdle(t *testing.T) {
	ac, _ := newTestController()
	ac.OnStreamMessage("assistant", "")
	ac.OnProcessExit(0)

	ac.mu.Lock()
	defer ac.mu.Unlock()
	if ac.status != models.AgentStatusIdle {
		t.Errorf("expected Idle on exit 0, got %s", ac.status)
	}
}

func TestOnProcessExit_NonZero_SetsError(t *testing.T) {
	ac, _ := newTestController()
	ac.OnProcessExit(1)

	ac.mu.Lock()
	defer ac.mu.Unlock()
	if ac.status != models.AgentStatusError {
		t.Errorf("expected Error on exit 1, got %s", ac.status)
	}
}

func TestOnHookRunning_SetsRunning(t *testing.T) {
	ac, _ := newTestController()
	ac.OnHookRunning()

	ac.mu.Lock()
	defer ac.mu.Unlock()
	if ac.status != models.AgentStatusRunning {
		t.Errorf("expected Running, got %s", ac.status)
	}
}

func TestOnHookIdle_SetsIdle(t *testing.T) {
	ac, _ := newTestController()
	ac.OnHookRunning()
	ac.OnHookIdle()

	ac.mu.Lock()
	defer ac.mu.Unlock()
	if ac.status != models.AgentStatusIdle {
		t.Errorf("expected Idle, got %s", ac.status)
	}
}

func TestOnHookBlocked_SetsInput(t *testing.T) {
	ac, _ := newTestController()
	ac.OnHookBlocked()

	ac.mu.Lock()
	defer ac.mu.Unlock()
	if ac.status != models.AgentStatusInput {
		t.Errorf("expected Input, got %s", ac.status)
	}
}

func TestOnHookError_SetsError(t *testing.T) {
	ac, _ := newTestController()
	ac.OnHookError()

	ac.mu.Lock()
	defer ac.mu.Unlock()
	if ac.status != models.AgentStatusError {
		t.Errorf("expected Error, got %s", ac.status)
	}
}

func TestOnStatusChanged_Callback_Fires(t *testing.T) {
	ac, id := newTestController()
	var gotID uuid.UUID
	var gotStatus models.AgentStatus
	ac.OnStatusChanged = func(agentID uuid.UUID, status models.AgentStatus) {
		gotID = agentID
		gotStatus = status
	}

	ac.OnStreamMessage("assistant", "")

	if gotID != id {
		t.Errorf("expected agent ID %s, got %s", id, gotID)
	}
	if gotStatus != models.AgentStatusRunning {
		t.Errorf("expected Running, got %s", gotStatus)
	}
}

func TestOnStatusChanged_NotCalledForSameStatus(t *testing.T) {
	ac, _ := newTestController()
	var callCount int
	ac.OnStatusChanged = func(_ uuid.UUID, _ models.AgentStatus) {
		callCount++
	}

	ac.OnStreamMessage("assistant", "")
	ac.OnStreamMessage("assistant", "") // same status — should NOT fire

	if callCount != 1 {
		t.Errorf("expected 1 callback call, got %d", callCount)
	}
}

func TestConcurrentStreamMessages_NoRace(t *testing.T) {
	ac, _ := newTestController()
	ac.OnTurnComplete = func() {}
	ac.OnStatusChanged = func(_ uuid.UUID, _ models.AgentStatus) {}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			ac.OnStreamMessage("assistant", "")
		}()
		go func() {
			defer wg.Done()
			ac.OnStreamMessage("result", "success")
		}()
		go func() {
			defer wg.Done()
			ac.OnStreamMessage("system", "init")
		}()
	}
	wg.Wait()
}
