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
	UpdateID      int            `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

type Message struct {
	MessageID int    `json:"message_id"`
	From      *User  `json:"from"`
	Chat      *Chat  `json:"chat"`
	Text      string `json:"text"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    *User    `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// InlineKeyboard types
type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

// ReplyKeyboard types
type KeyboardButton struct {
	Text string `json:"text"`
}

type ReplyKeyboardMarkup struct {
	Keyboard        [][]KeyboardButton `json:"keyboard"`
	ResizeKeyboard  bool               `json:"resize_keyboard"`
	OneTimeKeyboard bool               `json:"one_time_keyboard"`
	IsPersistent    bool               `json:"is_persistent"`
}

func (c *Client) SendMessage(chatID int64, text string) error {
	url := fmt.Sprintf(BaseURL+"/sendMessage", c.Token)

	body, err := json.Marshal(map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
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

func (c *Client) SendMessageWithKeyboard(chatID int64, text string, keyboard InlineKeyboardMarkup) error {
	url := fmt.Sprintf(BaseURL+"/sendMessage", c.Token)

	body, err := json.Marshal(map[string]interface{}{
		"chat_id":      chatID,
		"text":         text,
		"parse_mode":   "HTML",
		"reply_markup": keyboard,
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

func (c *Client) SendMessageWithReplyKeyboard(chatID int64, text string, keyboard ReplyKeyboardMarkup) error {
	url := fmt.Sprintf(BaseURL+"/sendMessage", c.Token)

	body, err := json.Marshal(map[string]interface{}{
		"chat_id":      chatID,
		"text":         text,
		"parse_mode":   "HTML",
		"reply_markup": keyboard,
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

func (c *Client) AnswerCallbackQuery(callbackQueryID string, text string) error {
	url := fmt.Sprintf(BaseURL+"/answerCallbackQuery", c.Token)

	body, err := json.Marshal(map[string]interface{}{
		"callback_query_id": callbackQueryID,
		"text":              text,
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

func (c *Client) EditMessageText(chatID int64, messageID int, text string, keyboard *InlineKeyboardMarkup) error {
	url := fmt.Sprintf(BaseURL+"/editMessageText", c.Token)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       text,
		"parse_mode": "HTML",
	}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}

	body, err := json.Marshal(payload)
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
