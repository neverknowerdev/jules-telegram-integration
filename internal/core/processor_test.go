package core

import (
	"context"
	"strings"
	"testing"

	"github.com/neverknowerdev/jules-telegram-bot/internal/firestore"
	"github.com/neverknowerdev/jules-telegram-bot/internal/jules"
	"github.com/neverknowerdev/jules-telegram-bot/internal/mocks"
)

func setupMocks() (*mocks.MockTelegramClient, *mocks.MockJulesClient, *mocks.MockFirestoreClient, *Processor) {
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

	p := NewProcessor(jc, fc, tc, nil)

	return tc, jc, fc, p
}

func TestProcessor_EmptyChats(t *testing.T) {
	tc, _, fc, p := setupMocks()

	chat := &firestore.ChatConfig{
		ChatID:   123,
		ThreadID: 456,
		// No current session
	}
	fc.Configs["123_456"] = chat

	err := p.ProcessChat(context.Background(), chat)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if len(tc.SentMessages) != 0 {
		t.Errorf("Expected no messages sent")
	}
}

func TestProcessor_NewActivities(t *testing.T) {
	tc, jc, fc, p := setupMocks()

	chat := &firestore.ChatConfig{
		ChatID:         123,
		ThreadID:       456,
		CurrentSession: "sessions/1",
		State:          "IN_PROGRESS",
		LastActivityID: "act0",
	}
	fc.Configs["123_456"] = chat

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

	err := p.ProcessChat(context.Background(), chat)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	foundAgentMessage := false
	for _, msg := range tc.SentMessages {
		if strings.Contains(msg, "Hello from Jules") {
			foundAgentMessage = true
		}
	}
	if !foundAgentMessage {
		t.Errorf("Expected agent message to be sent")
	}

	config := fc.Configs["123_456"]
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

	config.ProgressMessageID = 999
	jc.Activities = append(jc.Activities, jules.Activity{
		Id:         "act3",
		Originator: "system",
		ProgressUpdated: &struct {
			Title       string `json:"title"`
			Description string `json:"description"`
		}{Title: "Executing step 3", Description: "Checking more files..."},
	})

	err = p.ProcessChat(context.Background(), chat)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if len(tc.EditedMessages) == 0 {
		t.Errorf("Expected message to be edited")
	} else if !strings.Contains(tc.EditedMessages[0], "Executing step 3") {
		t.Errorf("Expected edited message to contain new content, got %s", tc.EditedMessages[0])
	}
}

func TestProcessor_SessionCompletedAndPR(t *testing.T) {
	tc, jc, fc, p := setupMocks()

	chat := &firestore.ChatConfig{
		ChatID:         123,
		ThreadID:       456,
		CurrentSession: "sessions/2",
		State:          "IN_PROGRESS",
		LastActivityID: "act0",
	}
	fc.Configs["123_456"] = chat

	jc.Activities = []jules.Activity{
		{
			Id:               "act1",
			Originator:       "system",
			SessionCompleted: &struct{}{},
		},
	}

	err := p.ProcessChat(context.Background(), chat)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

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

func TestProcessor_SessionFailed(t *testing.T) {
	tc, jc, fc, p := setupMocks()

	chat := &firestore.ChatConfig{
		ChatID:         123,
		ThreadID:       456,
		CurrentSession: "sessions/1",
		State:          "IN_PROGRESS",
		LastActivityID: "act0",
	}
	fc.Configs["123_456"] = chat

	jc.Activities = []jules.Activity{
		{
			Id:         "act1",
			Originator: "system",
			SessionFailed: &struct {
				Reason string `json:"reason"`
			}{Reason: "Test error"},
		},
	}

	err := p.ProcessChat(context.Background(), chat)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
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
