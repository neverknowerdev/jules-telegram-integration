package telegraph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

var BaseURL = "https://api.telegra.ph"

type Client struct {
	AccessToken string
	HTTPClient  *http.Client
}

func NewClient(accessToken string) *Client {
	return &Client{
		AccessToken: accessToken,
		HTTPClient:  &http.Client{},
	}
}

// Node represents a DOM node in Telegraph
type Node struct {
	Tag      string      `json:"tag"`
	Attrs    *NodeAttrs  `json:"attrs,omitempty"`
	Children []NodeChild `json:"children,omitempty"`
}

type NodeAttrs struct {
	Href string `json:"href,omitempty"`
	Src  string `json:"src,omitempty"`
}

// NodeChild can be a string or a Node
type NodeChild interface{}

type PageResponse struct {
	Ok     bool `json:"ok"`
	Result struct {
		Path        string `json:"path"`
		URL         string `json:"url"`
		Title       string `json:"title"`
		Description string `json:"description"`
		AuthorName  string `json:"author_name"`
		AuthorURL   string `json:"author_url"`
		Views       int    `json:"views"`
		CanEdit     bool   `json:"can_edit"`
	} `json:"result"`
}

type AccountResponse struct {
	Ok     bool `json:"ok"`
	Result struct {
		ShortName   string `json:"short_name"`
		AuthorName  string `json:"author_name"`
		AuthorURL   string `json:"author_url"`
		AccessToken string `json:"access_token"`
		AuthURL     string `json:"auth_url"`
	} `json:"result"`
}

func CreateAccount(shortName, authorName string) (*AccountResponse, error) {
	url := fmt.Sprintf("%s/createAccount?short_name=%s&author_name=%s", BaseURL, shortName, authorName)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var account AccountResponse
	if err := json.Unmarshal(body, &account); err != nil {
		return nil, err
	}
	if !account.Ok {
		return nil, fmt.Errorf("failed to create account: %s", string(body))
	}
	return &account, nil
}

func (c *Client) CreatePage(title string, content []Node) (*PageResponse, error) {
	payload := map[string]interface{}{
		"access_token": c.AccessToken,
		"title":        title,
		"content":      content,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	resp, err := c.HTTPClient.Post(BaseURL+"/createPage", "application/json", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var pageResp PageResponse
	if err := json.Unmarshal(body, &pageResp); err != nil {
		return nil, err
	}
	if !pageResp.Ok {
		return nil, fmt.Errorf("failed to create page: %s", string(body))
	}
	return &pageResp, nil
}

func (c *Client) EditPage(path, title string, content []Node) (*PageResponse, error) {
	payload := map[string]interface{}{
		"access_token": c.AccessToken,
		"title":        title,
		"content":      content,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	resp, err := c.HTTPClient.Post(fmt.Sprintf("%s/editPage/%s", BaseURL, path), "application/json", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var pageResp PageResponse
	if err := json.Unmarshal(body, &pageResp); err != nil {
		return nil, err
	}
	if !pageResp.Ok {
		return nil, fmt.Errorf("failed to edit page: %s", string(body))
	}
	return &pageResp, nil
}
