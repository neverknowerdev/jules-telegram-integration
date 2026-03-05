package poller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/neverknowerdev/jules-telegram-bot/internal/firestore"
	"github.com/neverknowerdev/jules-telegram-bot/internal/jules"
	"github.com/neverknowerdev/jules-telegram-bot/internal/mocks"
)

func setupMocks() (*mocks.MockTelegramClient, *mocks.MockJulesClient, *mocks.MockFirestoreClient) {
	tc := &mocks.MockTelegramClient{}
	jc := &mocks.MockJulesClient{
		Sessions: []jules.Session{
			{Name: "sessions/1", Title: "Fix bug", State: "IN_PROGRESS"},
			{Name: "sessions/2", Title: "Completed session", State: "COMPLETED", Outputs: []jules.SessionOutput{
				{PullRequest: &struct {
					URL     string `json:"url"`
					Title   string `json:"title"`
					HeadRef string `json:"headRef"`
					BaseRef string `json:"baseRef"`
				}{URL: "http://github.com/pr/1", Title: "Test PR", HeadRef: "feature", BaseRef: "main"}},
			}},
		},
	}
	fc := mocks.NewMockFirestoreClient()

	// Inject into the package-level variables
	telegramClient = tc
	julesClient = jc
	firestoreClient = fc
	projectID = "test-project" // Avoid calling initEnv()

	return tc, jc, fc
}

func sendPollerRequest(t *testing.T) *httptest.ResponseRecorder {
	req, _ := http.NewRequest("POST", "/", bytes.NewBuffer(nil))
	rr := httptest.NewRecorder()
	JulesPoller(rr, req)
	return rr
}

func TestPoller_EmptyChats(t *testing.T) {
	tc, _, _ := setupMocks()

	rr := sendPollerRequest(t)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
	if len(tc.SentMessages) != 0 {
		t.Errorf("Expected no messages sent")
	}
}

func TestPoller_NewActivities(t *testing.T) {
	tc, jc, fc := setupMocks()

	fc.Configs["123_456"] = &firestore.ChatConfig{
		ChatID:         123,
		ThreadID:       456,
		CurrentSession: "sessions/1",
		State:          "IN_PROGRESS",
		LastActivityID: "act0",
	}

	jc.Activities = []jules.Activity{
		{
			Id:         "act1",
			Originator: "agent",
			AgentMessaged: &struct {
				AgentMessage string `json:"agentMessage"`
			}{AgentMessage: "Hello from Jules"},
		},
		{
			Id:         "act2",
			Originator: "system",
			ProgressUpdated: &struct {
				Title       string `json:"title"`
				Description string `json:"description"`
			}{Title: "Thinking", Description: "Checking files..."},
		},
	}

	rr := sendPollerRequest(t)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	// Verify agent message sent
	foundAgentMessage := false
	for _, msg := range tc.SentMessages {
		if strings.Contains(msg, "Hello from Jules") {
			foundAgentMessage = true
		}
	}
	if !foundAgentMessage {
		t.Errorf("Expected agent message to be sent")
	}

	// Verify progress message updated
	config := fc.Configs["123_456"]
	// The mock telegram client returns 0 for SendMessageReturnID, so we expect 0 unless we explicitly mock a return ID
	// Let's modify the setup or check to reflect this.
	// We'll just verify the last activity ID is updated for now.
	if config.LastActivityID != "act2" {
		t.Errorf("Expected last activity to be updated to act2, got %s", config.LastActivityID)
	}

	foundProgressMessage := false
	for _, msg := range tc.SentMessages {
		if strings.Contains(msg, "Jules is working on it") {
			foundProgressMessage = true
		}
	}
	if !foundProgressMessage {
		t.Errorf("Expected progress message to be sent")
	}

	// Test if it edits existing message properly with timestamp
	config.ProgressMessageID = 999
	jc.Activities = append(jc.Activities, jules.Activity{
		Id: "act3",
		Originator: "system",
		ProgressUpdated: &struct {
			Title       string `json:"title"`
			Description string `json:"description"`
		}{Title: "Thinking Again", Description: "Checking more files..."},
	})
	rr = sendPollerRequest(t)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
	if len(tc.EditedMessages) == 0 {
		t.Errorf("Expected message to be edited")
	} else if !strings.Contains(tc.EditedMessages[0], "Last updated:") {
		t.Errorf("Expected edited message to contain timestamp, got %s", tc.EditedMessages[0])
	}
}

func TestPoller_SessionCompletedAndPR(t *testing.T) {
	tc, jc, fc := setupMocks()

	fc.Configs["123_456"] = &firestore.ChatConfig{
		ChatID:         123,
		ThreadID:       456,
		CurrentSession: "sessions/2", // This is mocked as COMPLETED with a PR output
		State:          "IN_PROGRESS",
		LastActivityID: "act0",
	}

	jc.Activities = []jules.Activity{
		{
			Id:               "act1",
			Originator:       "system",
			SessionCompleted: &struct{}{},
		},
	}

	rr := sendPollerRequest(t)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	// Expected to receive completion message and PR notification
	foundCompletion := false
	foundPR := false

	for _, msg := range tc.SentMessages {
		if strings.Contains(msg, "completed the task") {
			foundCompletion = true
		}
		if strings.Contains(msg, "New Pull Request Created") {
			foundPR = true
		}
	}

	if !foundCompletion {
		t.Errorf("Expected completion message")
	}
	if !foundPR {
		t.Errorf("Expected PR notification")
	}

	config := fc.Configs["123_456"]
	if config.State != "COMPLETED" {
		t.Errorf("Expected state to be updated to COMPLETED")
	}

	// Verify PR view button is present, instead of create PR
	foundViewPR := false
	for _, kb := range tc.SentKeyboards {
		for _, row := range kb.InlineKeyboard {
			for _, btn := range row {
				if strings.Contains(btn.Text, "View Pull Request") {
					foundViewPR = true
				}
				if strings.Contains(btn.Text, "Create PR") {
					t.Errorf("Expected Create PR button to be removed")
				}
			}
		}
	}
	if !foundViewPR {
		t.Errorf("Expected View Pull Request button")
	}
}

func TestPoller_SessionFailed(t *testing.T) {
	tc, jc, fc := setupMocks()

	fc.Configs["123_456"] = &firestore.ChatConfig{
		ChatID:         123,
		ThreadID:       456,
		CurrentSession: "sessions/1",
		State:          "IN_PROGRESS",
		LastActivityID: "act0",
	}

	jc.Activities = []jules.Activity{
		{
			Id:         "act1",
			Originator: "system",
			SessionFailed: &struct {
				Reason string `json:"reason"`
			}{Reason: "Test error"},
		},
	}

	rr := sendPollerRequest(t)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	foundError := false
	for _, msg := range tc.SentMessages {
		if strings.Contains(msg, "Test error") {
			foundError = true
		}
	}

	if !foundError {
		t.Errorf("Expected failure message")
	}
}
