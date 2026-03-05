package poller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/neverknowerdev/jules-telegram-bot/internal/jules"
)

func TestMockPollerLogic(t *testing.T) {
	// 1. Setup Mock Jules API
	julesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1alpha/sessions/123/activities" {
			// Mock ListActivities
			activities := map[string][]jules.Activity{
				"activities": {
					{
						Id:         "act2",
						Originator: "agent",
						AgentMessaged: struct {
							AgentMessage string `json:"agentMessage"`
						}{AgentMessage: "Done"},
					},
					{
						Id:         "act1",
						Originator: "user",
					},
				},
			}
			json.NewEncoder(w).Encode(activities)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer julesServer.Close()

	// 2. Override Jules BaseURL for testing
	originalURL := jules.BaseURL
	jules.BaseURL = julesServer.URL + "/v1alpha"
	defer func() { jules.BaseURL = originalURL }()

	// 3. Initialize Client
	client := jules.NewClient("test-key")

	// 4. Test logic mirroring poller
	activities, err := client.ListActivities("sessions/123")
	if err != nil {
		t.Fatalf("Failed to list activities: %v", err)
	}

	if len(activities) != 2 {
		t.Fatalf("Expected 2 activities, got %d", len(activities))
	}

	// Logic from poller to check if Jules finished
	isWaitingForJules := true
	julesFinished := false

	for _, act := range activities {
		if act.Originator == "agent" || act.Originator == "system" {
			if act.ProgressUpdated.Title == "" {
				julesFinished = true
			}
		}
	}

	if !julesFinished {
		t.Errorf("Expected julesFinished to be true based on AgentMessaged")
	}

	if isWaitingForJules && julesFinished {
		isWaitingForJules = false
	}

	if isWaitingForJules {
		t.Errorf("Expected isWaitingForJules to be false after processing julesFinished")
	}
}
