package poller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/neverknowerdev/jules-telegram-bot/internal/firestore"
	"github.com/neverknowerdev/jules-telegram-bot/internal/jules"
	"github.com/neverknowerdev/jules-telegram-bot/internal/telegraph"
)

func TestPoller_TelegraphIntegration(t *testing.T) {
	tc, jc, fc := setupMocks()

	// Add an active chat that has a current session
	fc.SaveChatConfig(context.Background(), firestore.ChatConfig{
		ChatID:         123,
		ThreadID:       456,
		CurrentSession: "sessions/1",
		LastActivityID: "sessions/1/activities/0",
		Source:         "repos/test-repo",
	})

	jc.Activities = []jules.Activity{
		{
			Name:       "sessions/1/activities/1",
			CreateTime: time.Now().Add(-2 * time.Minute).Format(time.RFC3339Nano),
			Originator: "agent",
			ProgressUpdated: &struct {
				Title       string `json:"title"`
				Description string `json:"description"`
			}{Title: "Step 1: Analyzed"},
		},
	}

	createPageCalled := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/createPage" {
			createPageCalled = true
			w.Write([]byte(`{"ok":true,"result":{"path":"Mock-Path","url":"https://telegra.ph/Mock-Path"}}`))
		} else {
			t.Errorf("Unexpected path %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	originalTelegraphBaseURL := telegraph.BaseURL
	telegraph.BaseURL = ts.URL
	defer func() { telegraph.BaseURL = originalTelegraphBaseURL }()

	// Re-init telegraphClient using mock URL
	telegraphClient = telegraph.NewClient("fake_token")

	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	JulesPoller(rr, req)

	if !createPageCalled {
		t.Errorf("Expected CreatePage to be called but it wasn't")
	}

	if len(tc.SentMessages) == 0 {
		t.Fatalf("Expected telegram message to be sent")
	}
	msg := tc.SentMessages[0]
	if !strings.Contains(msg, "Step 1: Analyzed") {
		t.Errorf("Expected message to contain 'Step 1: Analyzed'")
	}

	chatConfig, _ := fc.GetChatConfig(context.Background(), 123, 456)
	if len(chatConfig.TelegraphPages) == 0 || chatConfig.TelegraphPages[0] != "Mock-Path" {
		t.Errorf("Expected telegraph pages to be updated with Mock-Path")
	}
}
