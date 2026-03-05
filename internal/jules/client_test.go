package jules

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListSessions(t *testing.T) {
	// Mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1alpha/sessions" {
			t.Errorf("Expected path /v1alpha/sessions, got %s", r.URL.Path)
		}

		resp := ListSessionsResponse{
			Sessions: []Session{
				{Name: "sessions/1", Title: "Test Session", Id: "1"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Override BaseURL
	originalBaseURL := BaseURL
	BaseURL = server.URL + "/v1alpha"
	defer func() { BaseURL = originalBaseURL }()

	// Test
	client := NewClient("test-key")
	sessions, err := client.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	if len(sessions) != 1 {
		t.Fatalf("Expected 1 session, got %d", len(sessions))
	}

	if sessions[0].Name != "sessions/1" {
		t.Errorf("Expected session name 'sessions/1', got %s", sessions[0].Name)
	}
}

func TestListActivities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1alpha/sessions/1/activities" {
			t.Errorf("Expected path /v1alpha/sessions/1/activities, got %s", r.URL.Path)
		}

		resp := ListActivitiesResponse{
			Activities: []Activity{
				{Name: "sessions/1/activities/1", Id: "1", Originator: "agent"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	originalBaseURL := BaseURL
	BaseURL = server.URL + "/v1alpha"
	defer func() { BaseURL = originalBaseURL }()

	client := NewClient("test-key")
	activities, err := client.ListActivities("sessions/1", "")
	if err != nil {
		t.Fatalf("Failed to list activities: %v", err)
	}

	if len(activities) != 1 {
		t.Fatalf("Expected 1 activity, got %d", len(activities))
	}

	if activities[0].Id != "1" {
		t.Errorf("Expected activity ID '1', got %s", activities[0].Id)
	}
}

func TestSendMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1alpha/sessions/1:sendMessage" {
			t.Errorf("Expected path /v1alpha/sessions/1:sendMessage, got %s", r.URL.Path)
		}

		var req SendMessageRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Prompt != "hello" {
			t.Errorf("Expected prompt 'hello', got %s", req.Prompt)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	originalBaseURL := BaseURL
	BaseURL = server.URL + "/v1alpha"
	defer func() { BaseURL = originalBaseURL }()

	client := NewClient("test-key")
	err := client.SendMessage("sessions/1", "hello")
	if err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}
}
