package poller

import (
	"strings"
	"testing"

	"github.com/neverknowerdev/jules-telegram-bot/internal/jules"
)

func TestFormatActivity(t *testing.T) {
	tests := []struct {
		name     string
		activity jules.Activity
		expected string
	}{
		{
			name: "User message",
			activity: jules.Activity{
				Originator: "user",
				UserMessaged: struct {
					UserMessage string `json:"userMessage"`
				}{UserMessage: "hello"},
			},
			expected: "🤖 <b>You</b>\nhello",
		},
		{
			name: "Agent message",
			activity: jules.Activity{
				Originator: "agent",
				AgentMessaged: struct {
					AgentMessage string `json:"agentMessage"`
				}{AgentMessage: "hi there"},
			},
			expected: "🤖 <b>Jules</b>\nhi there",
		},
		{
			name: "Progress update",
			activity: jules.Activity{
				Originator: "agent",
				ProgressUpdated: struct {
					Title       string `json:"title"`
					Description string `json:"description"`
				}{Title: "Working", Description: "Checking files"},
			},
			expected: "🤖 <b>Working</b>\nChecking files",
		},
		{
			name: "HTML Escape",
			activity: jules.Activity{
				Originator: "agent",
				ProgressUpdated: struct {
					Title       string `json:"title"`
					Description string `json:"description"`
				}{Title: "<Working>", Description: ""},
			},
			expected: "🤖 <b>&lt;Working&gt;</b>",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatActivity(tc.activity)
			if !strings.Contains(got, tc.expected) {
				t.Errorf("Expected %q to contain %q", got, tc.expected)
			}
		})
	}
}

func TestIsJulesFinished(t *testing.T) {
	// Testing the logic used in main.go for checking if Jules is done
	activities := []jules.Activity{
		{
			Originator: "agent",
			ProgressUpdated: struct {
				Title       string `json:"title"`
				Description string `json:"description"`
			}{Title: "Working"},
		},
		{
			Originator: "agent",
			AgentMessaged: struct {
				AgentMessage string `json:"agentMessage"`
			}{AgentMessage: "Done"},
		},
	}

	// First activity (ProgressUpdated) -> not finished
	act := activities[0]
	finished := false
	if act.Originator == "agent" || act.Originator == "system" {
		if act.ProgressUpdated.Title == "" {
			finished = true
		}
	}
	if finished {
		t.Errorf("Expected ProgressUpdated to not mark Jules as finished")
	}

	// Second activity (AgentMessaged) -> finished
	act = activities[1]
	finished = false
	if act.Originator == "agent" || act.Originator == "system" {
		if act.ProgressUpdated.Title == "" {
			finished = true
		}
	}
	if !finished {
		t.Errorf("Expected AgentMessaged to mark Jules as finished")
	}
}
