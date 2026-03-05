package jules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

var BaseURL = "https://jules.googleapis.com/v1alpha"

type Client struct {
	ApiKey string
	HTTP   *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		ApiKey: apiKey,
		HTTP:   &http.Client{},
	}
}

type Source struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Id          string `json:"id"`
	GithubRepo  struct {
		Owner string `json:"owner"`
		Repo  string `json:"repo"`
	} `json:"githubRepo"`
}

type ListSourcesResponse struct {
	Sources       []Source `json:"sources"`
	NextPageToken string   `json:"nextPageToken"`
}

func (c *Client) ListSources() ([]Source, error) {
	var allSources []Source
	pageToken := ""

	for {
		u, _ := url.Parse(BaseURL + "/sources")
		q := u.Query()
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}
		u.RawQuery = q.Encode()

		req, err := http.NewRequest("GET", u.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-Goog-Api-Key", c.ApiKey)

		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("Jules API error: %s", string(body))
		}

		var result ListSourcesResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}

		allSources = append(allSources, result.Sources...)
		pageToken = result.NextPageToken
		if pageToken == "" {
			break
		}
	}
	return allSources, nil
}

type SessionOutput struct {
	PullRequest *struct {
		URL     string `json:"url"`
		Title   string `json:"title"`
		HeadRef string `json:"headRef"`
		BaseRef string `json:"baseRef"`
	} `json:"pullRequest,omitempty"`
	ChangeSet *struct {
		Source   string `json:"source"`
		GitPatch struct {
			SuggestedCommitMessage string `json:"suggestedCommitMessage"`
		} `json:"gitPatch"`
	} `json:"changeSet,omitempty"`
}

type Session struct {
	Name          string          `json:"name"`
	Id            string          `json:"id"`
	Title         string          `json:"title"`
	UpdateTime    string          `json:"updateTime"`
	State         string          `json:"state"`
	URL           string          `json:"url"`
	Outputs       []SessionOutput `json:"outputs,omitempty"`
	SourceContext struct {
		Source string `json:"source"`
	} `json:"sourceContext"`
}

type ListSessionsResponse struct {
	Sessions      []Session `json:"sessions"`
	NextPageToken string    `json:"nextPageToken"`
}

func (c *Client) ListSessions() ([]Session, error) {
	var allSessions []Session
	pageToken := ""

	for {
		u, _ := url.Parse(BaseURL + "/sessions")
		q := u.Query()
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}
		u.RawQuery = q.Encode()

		req, err := http.NewRequest("GET", u.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-Goog-Api-Key", c.ApiKey)

		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("Jules API error: %s", string(body))
		}

		var result ListSessionsResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}

		allSessions = append(allSessions, result.Sessions...)
		pageToken = result.NextPageToken
		if pageToken == "" {
			break
		}
	}
	return allSessions, nil
}

func (c *Client) GetSession(sessionName string) (*Session, error) {
	if !strings.HasPrefix(sessionName, "sessions/") {
		sessionName = "sessions/" + sessionName
	}
	req, err := http.NewRequest("GET", BaseURL+"/"+sessionName, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Goog-Api-Key", c.ApiKey)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Jules API error: %s", string(body))
	}

	var session Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, err
	}
	return &session, nil
}

type Activity struct {
	Name            string `json:"name"`
	Id              string `json:"id"`
	CreateTime      string `json:"createTime"`
	Originator      string `json:"originator"`
	ProgressUpdated *struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	} `json:"progressUpdated,omitempty"`
	PlanGenerated *struct {
		Plan struct {
			Id    string `json:"id"`
			Title string `json:"title"`
			Steps []struct {
				Title       string `json:"title"`
				Description string `json:"description"`
			} `json:"steps"`
		} `json:"plan"`
	} `json:"planGenerated,omitempty"`
	AgentMessaged *struct {
		AgentMessage string `json:"agentMessage"`
	} `json:"agentMessaged,omitempty"`
	UserMessaged *struct {
		UserMessage string `json:"userMessage"`
	} `json:"userMessaged,omitempty"`
	SessionCompleted *struct{} `json:"sessionCompleted,omitempty"`
	SessionFailed    *struct {
		Reason string `json:"reason"`
	} `json:"sessionFailed,omitempty"`
	Artifacts []struct {
		ChangeSet struct {
			Source string `json:"source"`
		} `json:"changeSet"`
	} `json:"artifacts,omitempty"`
}

type ListActivitiesResponse struct {
	Activities    []Activity `json:"activities"`
	NextPageToken string     `json:"nextPageToken"`
}

func (c *Client) ListActivities(sessionName string, sinceID string) ([]Activity, error) {
	var filteredActivities []Activity
	pageToken := ""
	foundSince := false

	// The sessionName is usually "sessions/123", we need to append "/activities"
	endpoint := fmt.Sprintf("%s/%s/activities", BaseURL, sessionName)

	for {
		u, _ := url.Parse(endpoint)
		q := u.Query()
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}
		// Use a very small page size to keep memory peaks low per API call
		q.Set("pageSize", "10")
		u.RawQuery = q.Encode()

		req, err := http.NewRequest("GET", u.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-Goog-Api-Key", c.ApiKey)

		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("Jules API error: %s", string(body))
		}

		var result ListActivitiesResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}

		if len(result.Activities) > 0 {
			if sinceID != "" && !foundSince {
				// Search for sinceID in this page
				for i, act := range result.Activities {
					if act.Id == sinceID {
						foundSince = true
						// Keep everything AFTER sinceID
						if i+1 < len(result.Activities) {
							filteredActivities = append(filteredActivities, result.Activities[i+1:]...)
						}
						break
					}
				}
				// If sinceID not found in this page, we don't keep these activities
				// but we continue to the next page.
			} else if sinceID != "" && foundSince {
				// already found sinceID, keep everything in subsequent pages
				filteredActivities = append(filteredActivities, result.Activities...)
			} else {
				// No sinceID provided (first run). Just keep the latest few items.
				// We keep at most 20 items to have enough for a summary if needed.
				filteredActivities = append(filteredActivities, result.Activities...)
				if len(filteredActivities) > 20 {
					filteredActivities = filteredActivities[len(filteredActivities)-20:]
				}
			}
		}

		pageToken = result.NextPageToken
		if pageToken == "" {
			break
		}
	}
	return filteredActivities, nil
}

type SendMessageRequest struct {
	Prompt string `json:"prompt"`
}

func (c *Client) SendMessage(sessionName, message string) error {
	endpoint := fmt.Sprintf("%s/%s:sendMessage", BaseURL, sessionName)

	reqBody := SendMessageRequest{Prompt: message}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("X-Goog-Api-Key", c.ApiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Jules API error: %s", string(respBody))
	}

	return nil
}

type CreateSessionRequest struct {
	Prompt              string                 `json:"prompt"`
	SourceContext       map[string]interface{} `json:"sourceContext,omitempty"`
	RequirePlanApproval *bool                  `json:"requirePlanApproval,omitempty"`
	AutomationMode      string                 `json:"automationMode,omitempty"`
}

func (c *Client) CreateSession(prompt, source, mode string) (*Session, error) {
	endpoint := BaseURL + "/sessions"

	reqBody := CreateSessionRequest{
		Prompt: prompt,
	}
	if source != "" {
		reqBody.SourceContext = map[string]interface{}{
			"source": source,
		}
		if strings.Contains(source, "github") {
			reqBody.SourceContext["githubRepoContext"] = map[string]interface{}{
				"startingBranch": "main",
			}
		}
	}

	// Jules always creates a PR by default for this integration.
	reqBody.AutomationMode = "AUTO_CREATE_PR"

	if mode == "interactive" {
		b := true
		reqBody.RequirePlanApproval = &b
	} else {
		// start, scheduled, review all default to auto-approved plans.
		b := false
		reqBody.RequirePlanApproval = &b
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Goog-Api-Key", c.ApiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Jules API error: %s", string(respBody))
	}

	var session Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, err
	}

	return &session, nil
}

func (c *Client) ArchiveSession(sessionName string) error {
	endpoint := fmt.Sprintf("%s/%s:archive", BaseURL, sessionName)

	req, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Goog-Api-Key", c.ApiKey)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Jules API error: %s", string(respBody))
	}

	return nil
}

func (c *Client) ApprovePlan(sessionName string) error {
	endpoint := fmt.Sprintf("%s/%s:approvePlan", BaseURL, sessionName)

	req, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Goog-Api-Key", c.ApiKey)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Jules API error: %s", string(respBody))
	}

	return nil
}
