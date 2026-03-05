package telegraph

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateAccount(t *testing.T) {
	mockResponse := `{
		"ok": true,
		"result": {
			"short_name": "JulesBot",
			"author_name": "Jules",
			"author_url": "",
			"access_token": "mocked_access_token",
			"auth_url": "https://edit.telegra.ph/auth/mocked"
		}
	}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/createAccount" {
			t.Errorf("Expected path /createAccount, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("short_name") != "JulesBot" {
			t.Errorf("Expected short_name JulesBot, got %s", r.URL.Query().Get("short_name"))
		}
		fmt.Fprintln(w, mockResponse)
	}))
	defer ts.Close()

	// temporarily override BaseURL for testing
	originalBaseURL := BaseURL
	BaseURL = ts.URL
	defer func() { BaseURL = originalBaseURL }()

	resp, err := CreateAccount("JulesBot", "Jules")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if resp.Result.AccessToken != "mocked_access_token" {
		t.Errorf("Expected token mocked_access_token, got %s", resp.Result.AccessToken)
	}
}
