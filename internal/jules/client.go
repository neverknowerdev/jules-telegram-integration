package jules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var BaseURL = "https://jules.googleapis.com/v1alpha"

type Client struct {
	ApiKey string
	HTTP   *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		ApiKey: apiKey,
		HTTP:   &http.Client{Timeout: 15 * time.Second},
	}
}

type SourceSummary struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	GithubRepo  struct {
		Repo string `json:"repo"`
	} `json:"githubRepo"`
}

type ListSourcesSummaryResponse struct {
	Sources       []SourceSummary `json:"sources"`
	NextPageToken string          `json:"nextPageToken"`
}

type Source struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Id          string `json:"id"`
	GithubRepo  struct {
		Owner         string `json:"owner"`
		Repo          string `json:"repo"`
		DefaultBranch struct {
			DisplayName string `json:"displayName"`
		} `json:"defaultBranch"`
		Branches []struct {
			DisplayName string `json:"displayName"`
		} `json:"branches"`
	} `json:"githubRepo"`
}

type ListSourcesResponse struct {
	Sources       []Source `json:"sources"`
	NextPageToken string   `json:"nextPageToken"`
}

func (c *Client) ListSourcesSummary() ([]SourceSummary, error) {
	var allSources []SourceSummary
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

		var result ListSourcesSummaryResponse
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

func (c *Client) GetSource(sourceName string) (*Source, error) {
	if !strings.HasPrefix(sourceName, "sources/") {
		sourceName = "sources/" + sourceName
	}
	url := BaseURL + "/" + sourceName
	req, err := http.NewRequest("GET", url, nil)
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
		log.Printf("[JULES] GetSource API error: %d %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("Jules API error: %s", string(body))
	}

	var source Source
	if err := json.NewDecoder(resp.Body).Decode(&source); err != nil {
		return nil, err
	}
	return &source, nil
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

		// Important: If activities are returned in chronological order (oldest first),
		// we must collect all pages first, OR we iterate until we find `sinceID` and then keep the rest.
		// Wait, if it's oldest first:
		// Page 1: 1, 2, 3 (sinceID=2). We keep 3.
		// Page 2: 4, 5, 6. We keep all.
		// This logic is correct for "oldest first".

		if len(result.Activities) > 0 {
			if sinceID != "" && !foundSince {
				for i, act := range result.Activities {
					if act.Id == sinceID {
						foundSince = true
						if i+1 < len(result.Activities) {
							filteredActivities = append(filteredActivities, result.Activities[i+1:]...)
						}
						break
					}
				}
			} else if sinceID != "" && foundSince {
				filteredActivities = append(filteredActivities, result.Activities...)
			} else {
				filteredActivities = append(filteredActivities, result.Activities...)
				if len(filteredActivities) > 20 {
					filteredActivities = filteredActivities[len(filteredActivities)-20:]
				}
			}
		} else {
			// If len == 0, we can break early
			break
		}

		pageToken = result.NextPageToken
		if pageToken == "" {
			break
		}

		// If we found sinceID, we already appended the rest of the current page.
		// Any subsequent pages will be entirely NEW activities since they are chronologically AFTER this page.
		// Wait! Are subsequent pages AFTER this page, or BEFORE?
		// Usually page 1 = oldest, page 2 = next oldest, so yes, they are AFTER.
		// Wait, no. If we use `pageToken`, the API usually orders consistently.
		// If we already found `sinceID`, we DO want subsequent pages because they contain even newer activities.
		// The previous logic broke if foundSince == true: `if pageToken == "" || (sinceID != "" && foundSince) { break }`.
		// This was a BUG! If sinceID is found on page 1, breaking means we ignore page 2 and page 3 (which are newer activities).
		// We should ONLY break if there is no next page token.
	}

	// Fallback: if we were looking for a specific ID but didn't find it (maybe it was deleted or expired),
	// we should return the latest activities so the poller isn't stuck forever.
	if sinceID != "" && !foundSince && len(filteredActivities) == 0 {
		log.Printf("[JULES] ListActivities: sinceID %q not found in any page for %s. Falling back to latest 20 activities.", sinceID, sessionName)

		// Fetch all activities again (or just return the last page we have, but let's be thorough)
		all, err := c.ListAllActivities(sessionName)
		if err == nil && len(all) > 0 {
			if len(all) > 20 {
				return all[len(all)-20:], nil
			}
			return all, nil
		}
	}

	return filteredActivities, nil
}

func (c *Client) ListAllActivities(sessionName string) ([]Activity, error) {
	var allActivities []Activity
	pageToken := ""

	endpoint := fmt.Sprintf("%s/%s/activities", BaseURL, sessionName)

	for {
		u, _ := url.Parse(endpoint)
		q := u.Query()
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}
		q.Set("pageSize", "100")
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

		allActivities = append(allActivities, result.Activities...)
		pageToken = result.NextPageToken
		if pageToken == "" {
			break
		}
	}
	return allActivities, nil
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

func (c *Client) CreateSession(prompt, source, mode, branch string) (*Session, error) {
	endpoint := BaseURL + "/sessions"

	reqBody := CreateSessionRequest{
		Prompt: prompt,
	}
	if source != "" {
		reqBody.SourceContext = map[string]interface{}{
			"source": source,
		}
		if strings.Contains(source, "github") {
			targetBranch := "main"
			if branch != "" {
				targetBranch = branch
			}
			reqBody.SourceContext["githubRepoContext"] = map[string]interface{}{
				"startingBranch": targetBranch,
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
