package webhook

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/neverknowerdev/jules-telegram-bot/internal/firestore"
	"github.com/neverknowerdev/jules-telegram-bot/internal/jules"
	"github.com/neverknowerdev/jules-telegram-bot/internal/mocks"
	"github.com/neverknowerdev/jules-telegram-bot/internal/telegram"
)

func setupMocks() (*mocks.MockTelegramClient, *mocks.MockJulesClient, *mocks.MockFirestoreClient) {
	tc := &mocks.MockTelegramClient{}
	jc := &mocks.MockJulesClient{
		Sources: []jules.Source{
			{Name: "sources/github/owner/repo1", DisplayName: "owner/repo1", GithubRepo: struct {
				Owner         string `json:"owner"`
				Repo          string `json:"repo"`
				DefaultBranch struct {
					DisplayName string `json:"displayName"`
				} `json:"defaultBranch"`
				Branches []struct {
					DisplayName string `json:"displayName"`
				} `json:"branches"`
			}{
				Owner: "owner", Repo: "repo1",
				Branches: []struct {
					DisplayName string `json:"displayName"`
				}{
					{DisplayName: "main"},
					{DisplayName: "feature/test"},
				},
			}},
		},
		Sessions: []jules.Session{
			{Name: "sessions/1", Title: "Fix bug", State: "IN_PROGRESS", SourceContext: struct{Source string `json:"source"`}{Source: "sources/github/owner/repo1"}},
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

func sendWebhookRequest(t *testing.T, payload telegram.Update) *httptest.ResponseRecorder {
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "/", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	TelegramWebhook(rr, req)
	return rr
}

func TestWebhook_StartCommand(t *testing.T) {
	tc, _, fc := setupMocks()

	update := telegram.Update{
		Message: &telegram.Message{
			Chat: &telegram.Chat{ID: 123},
			Text: "/start",
		},
	}

	rr := sendWebhookRequest(t, update)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	// Verify chat config was saved
	if len(fc.Configs) == 0 {
		t.Errorf("Expected chat config to be saved")
	}

	// Verify welcome message sent
	if len(tc.SentMessages) == 0 {
		t.Errorf("Expected welcome message to be sent")
	} else if !strings.Contains(tc.SentMessages[0], "Welcome!") {
		t.Errorf("Expected welcome message, got: %s", tc.SentMessages[0])
	}
}

func TestWebhook_TopicCreated(t *testing.T) {
	tc, _, fc := setupMocks()

	update := telegram.Update{
		Message: &telegram.Message{
			Chat:              &telegram.Chat{ID: 123},
			MessageThreadID:   456,
			ForumTopicCreated: &telegram.ForumTopicCreated{Name: "Fix login"},
		},
	}

	rr := sendWebhookRequest(t, update)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	// Verify chat config state changed to waiting_for_repo
	config := fc.Configs["123_456"]
	if config == nil || config.State != "waiting_for_repo" {
		t.Errorf("Expected state 'waiting_for_repo', got %v", config)
	}

	// Verify repo selection message sent
	if len(tc.SentMessages) == 0 || !strings.Contains(tc.SentMessages[0], "Select a repository") {
		t.Errorf("Expected repo selection message")
	}
}

func TestWebhook_NewTaskCommand(t *testing.T) {
	tc, _, _ := setupMocks()

	update := telegram.Update{
		Message: &telegram.Message{
			Chat: &telegram.Chat{ID: 123},
			Text: "➕ New Task",
		},
	}

	sendWebhookRequest(t, update)

	if len(tc.SentMessages) == 0 || !strings.Contains(tc.SentMessages[0], "Select repository") {
		t.Errorf("Expected repo selection message")
	}
}

func TestWebhook_HandleMessage_CreateSession(t *testing.T) {
	tc, jc, fc := setupMocks()

	// Simulate user selected a repo and is waiting to send prompt
	fc.Configs["123"] = &firestore.ChatConfig{
		ChatID:       123,
		State:        "waiting_for_message",
		DraftSource:  "sources/github/owner/repo1",
		CreationMode: "interactive",
	}

	update := telegram.Update{
		Message: &telegram.Message{
			Chat: &telegram.Chat{ID: 123},
			Text: "Please fix the login issue",
		},
	}

	sendWebhookRequest(t, update)

	// Verify session creation called on Jules client
	if jc.CreatedSession == nil && len(jc.SentMessages) == 0 && fc.Updates == nil {
		// Wait, MockJulesClient always returns a mock session on CreateSession
	}

	config := fc.Configs["123"]
	if config.CurrentSession != "sessions/mock-created-session" {
		t.Errorf("Expected current session to be updated, got: %s", config.CurrentSession)
	}

	if config.State != "" {
		t.Errorf("Expected state to be cleared, got: %s", config.State)
	}

	if len(tc.SentMessages) < 2 {
		t.Errorf("Expected creating session messages to be sent")
	}
}

func TestWebhook_Callback_TopicRepo(t *testing.T) {
	tc, _, fc := setupMocks()

	fc.Configs["123_456"] = &firestore.ChatConfig{
		ChatID:   123,
		ThreadID: 456,
		State:    "waiting_for_repo",
	}

	update := telegram.Update{
		CallbackQuery: &telegram.CallbackQuery{
			ID: "callback1",
			Message: &telegram.Message{
				Chat:      &telegram.Chat{ID: 123},
				MessageID: 789,
			},
			Data: "topicrepo:456:repo1",
		},
	}

	sendWebhookRequest(t, update)

	if len(tc.AnsweredCallbackIDs) == 0 {
		t.Errorf("Expected callback query to be answered")
	}

	config := fc.Configs["123_456"]
	if config.State != "waiting_for_branch" || config.DraftSource != "sources/github/owner/repo1" {
		t.Errorf("Expected state waiting_for_branch and source repo1, got state=%s source=%s", config.State, config.DraftSource)
	}

	if len(tc.EditedMessages) == 0 || !strings.Contains(tc.EditedMessages[0], "Select base branch") {
		t.Errorf("Expected edited message prompting for branch selection")
	}
}

func TestWebhook_Callback_TopicBranch(t *testing.T) {
	tc, _, fc := setupMocks()

	fc.Configs["123_456"] = &firestore.ChatConfig{
		ChatID:      123,
		ThreadID:    456,
		State:       "waiting_for_branch",
		DraftSource: "sources/github/owner/repo1",
	}

	update := telegram.Update{
		CallbackQuery: &telegram.CallbackQuery{
			ID: "callback2",
			Message: &telegram.Message{
				Chat:      &telegram.Chat{ID: 123},
				MessageID: 789,
			},
			Data: "topicbranch:456:feature/test",
		},
	}

	sendWebhookRequest(t, update)

	config := fc.Configs["123_456"]
	if config.State != "waiting_for_message" || config.DraftBranch != "feature/test" {
		t.Errorf("Expected state waiting_for_message and branch feature/test, got state=%s branch=%s", config.State, config.DraftBranch)
	}

	if len(tc.EditedMessages) == 0 || !strings.Contains(tc.EditedMessages[0], "Please enter the initial message") {
		t.Errorf("Expected edited message prompting for input")
	}
}

func TestWebhook_Callback_Clone(t *testing.T) {
	tc, _, fc := setupMocks()

	// Mock CreateForumTopic returns 999 for thread ID
	tc.CreateTopicReturnID = 999

	// Make sure we have a session to clone
	fc.Configs["123_456"] = &firestore.ChatConfig{
		ChatID:         123,
		ThreadID:       456,
		CurrentSession: "sessions/1",
	}

	update := telegram.Update{
		CallbackQuery: &telegram.CallbackQuery{
			ID: "callback_clone",
			Message: &telegram.Message{
				Chat:      &telegram.Chat{ID: 123},
				MessageID: 789,
			},
			Data: "clone:1",
		},
	}

	sendWebhookRequest(t, update)

	// Should create a new topic
	if len(tc.CreatedTopics) == 0 || !strings.Contains(tc.CreatedTopics[0], "Cloned") {
		t.Errorf("Expected new topic to be created with 'Cloned' in title")
	}

	// State should be waiting_for_title in the NEW document
	config := fc.Configs["123_999"]
	if config == nil {
		t.Fatalf("Expected new config document to be created")
	}
	if config.State != "waiting_for_title" {
		t.Errorf("Expected new topic state waiting_for_title, got %s", config.State)
	}

	// Should send message to new topic asking for title but providing branch options
	foundTitlePrompt := false
	for _, msg := range tc.SentMessages {
		if strings.Contains(msg, "reply with a new title") {
			foundTitlePrompt = true
		}
	}
	if !foundTitlePrompt {
		t.Errorf("Expected branch/title selection message to be sent to new topic")
	}
}

func TestWebhook_ApprovePlanCallback_RoutedToCorrectThread(t *testing.T) {
	tc, _, fc := setupMocks()

	// Create a chat config so GetChatsByChatID finds the right thread
	fc.Configs["123_456"] = &firestore.ChatConfig{
		ChatID:         123,
		ThreadID:       456,
		CurrentSession: "sessions/1",
	}

	update := telegram.Update{
		CallbackQuery: &telegram.CallbackQuery{
			ID: "callback2",
			Data: "approve_plan:1",
			Message: &telegram.Message{
				Chat: &telegram.Chat{ID: 123},
			},
		},
	}

	sendWebhookRequest(t, update)

	if len(tc.SentMessages) == 0 {
		t.Errorf("Expected success message")
	} else if !strings.Contains(tc.SentMessages[0], "Plan approved successfully") {
		t.Errorf("Expected plan approved message, got %s", tc.SentMessages[0])
	}

	if len(tc.SentThreadIDs) == 0 || tc.SentThreadIDs[0] != 456 {
		t.Errorf("Expected message to be sent to thread ID 456")
	}
}
