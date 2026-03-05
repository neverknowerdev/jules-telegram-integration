package telegram

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendMessage(t *testing.T) {
	token := "123:test"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := fmt.Sprintf("/bot%s/sendMessage", token)
		if r.URL.Path != expectedPath {
			t.Errorf("Expected path %s, got %s", expectedPath, r.URL.Path)
		}

		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)

		if body["text"] != "hello" {
			t.Errorf("Expected text 'hello', got %v", body["text"])
		}

		// ChatID comes as float64
		if fmt.Sprintf("%v", body["chat_id"]) != "123" {
			t.Errorf("Expected chat_id 123, got %v", body["chat_id"])
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok": true}`))
	}))
	defer server.Close()

	originalBaseURL := BaseURL
	// BaseURL is "https://api.telegram.org/bot%s"
	// We replace it with "http://127.0.0.1:12345/bot%s"
	// So fmt.Sprintf(BaseURL+"/sendMessage", token) becomes "http://127.0.0.1:12345/bot123:test/sendMessage"
	BaseURL = server.URL + "/bot%s"
	defer func() { BaseURL = originalBaseURL }()

	client := NewClient(token)
	err := client.SendMessage(123, 0, "hello")
	if err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}
}

func TestSendMessageWithThreadID(t *testing.T) {
	token := "123:test"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := fmt.Sprintf("/bot%s/sendMessage", token)
		if r.URL.Path != expectedPath {
			t.Errorf("Expected path %s, got %s", expectedPath, r.URL.Path)
		}

		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)

		if body["text"] != "hello thread" {
			t.Errorf("Expected text 'hello thread', got %v", body["text"])
		}

		if fmt.Sprintf("%v", body["message_thread_id"]) != "456" {
			t.Errorf("Expected message_thread_id 456, got %v", body["message_thread_id"])
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok": true}`))
	}))
	defer server.Close()

	originalBaseURL := BaseURL
	BaseURL = server.URL + "/bot%s"
	defer func() { BaseURL = originalBaseURL }()

	client := NewClient(token)
	err := client.SendMessage(123, 456, "hello thread")
	if err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}
}

func TestDeleteForumTopic(t *testing.T) {
	token := "123:test"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := fmt.Sprintf("/bot%s/deleteForumTopic", token)
		if r.URL.Path != expectedPath {
			t.Errorf("Expected path %s, got %s", expectedPath, r.URL.Path)
		}

		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)

		if fmt.Sprintf("%v", body["chat_id"]) != "123" {
			t.Errorf("Expected chat_id 123, got %v", body["chat_id"])
		}

		if fmt.Sprintf("%v", body["message_thread_id"]) != "456" {
			t.Errorf("Expected message_thread_id 456, got %v", body["message_thread_id"])
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok": true}`))
	}))
	defer server.Close()

	originalBaseURL := BaseURL
	BaseURL = server.URL + "/bot%s"
	defer func() { BaseURL = originalBaseURL }()

	client := NewClient(token)
	err := client.DeleteForumTopic(123, 456)
	if err != nil {
		t.Fatalf("Failed to delete forum topic: %v", err)
	}
}

func TestSetWebhook(t *testing.T) {
	token := "123:test"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := fmt.Sprintf("/bot%s/setWebhook", token)
		if r.URL.Path != expectedPath {
			t.Errorf("Expected path %s, got %s", expectedPath, r.URL.Path)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok": true}`))
	}))
	defer server.Close()

	originalBaseURL := BaseURL
	BaseURL = server.URL + "/bot%s"
	defer func() { BaseURL = originalBaseURL }()

	client := NewClient(token)
	err := client.SetWebhook("https://example.com/webhook")
	if err != nil {
		t.Fatalf("Failed to set webhook: %v", err)
	}
}
