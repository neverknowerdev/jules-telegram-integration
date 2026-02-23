package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

var BaseURL = "https://api.telegram.org/bot%s"

type Client struct {
	Token string
	HTTP  *http.Client
}

func NewClient(token string) *Client {
	return &Client{
		Token: token,
		HTTP:  &http.Client{},
	}
}

type Update struct {
	UpdateID int `json:"update_id"`
	Message  *Message `json:"message"`
}

type Message struct {
	MessageID int `json:"message_id"`
	From      *User `json:"from"`
	Chat      *Chat `json:"chat"`
	Text      string `json:"text"`
}

type User struct {
	ID        int64 `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

type Chat struct {
	ID   int64 `json:"id"`
	Type string `json:"type"`
}

func (c *Client) SendMessage(chatID int64, text string) error {
	url := fmt.Sprintf(BaseURL+"/sendMessage", c.Token)

	body, err := json.Marshal(map[string]interface{}{
		"chat_id": chatID,
		"text":    text,
	})
	if err != nil {
		return err
	}

	resp, err := c.HTTP.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API error: %d", resp.StatusCode)
	}

	return nil
}

func (c *Client) SetWebhook(webhookURL string) error {
	url := fmt.Sprintf(BaseURL+"/setWebhook", c.Token)

	body, err := json.Marshal(map[string]interface{}{
		"url": webhookURL,
	})
	if err != nil {
		return err
	}

	resp, err := c.HTTP.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API error: %d", resp.StatusCode)
	}
	return nil
}
