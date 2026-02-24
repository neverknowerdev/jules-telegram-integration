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
	MessageID      int      `json:"message_id"`
	From           *User    `json:"from"`
	Chat           *Chat    `json:"chat"`
	Text           string   `json:"text"`
	ReplyToMessage *Message `json:"reply_to_message"`
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

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

type SendMessageRequest struct {
	ChatID      int64       `json:"chat_id"`
	Text        string      `json:"text"`
	ParseMode   string      `json:"parse_mode,omitempty"`
	ReplyMarkup interface{} `json:"reply_markup,omitempty"`
}

func (c *Client) SendMessage(chatID int64, text string, markup ...interface{}) (int, error) {
	url := fmt.Sprintf(BaseURL+"/sendMessage", c.Token)

	req := SendMessageRequest{
		ChatID:    chatID,
		Text:      text,
		ParseMode: "Markdown", // Consider changing to MarkdownV2 or HTML if needed
	}

	if len(markup) > 0 {
		req.ReplyMarkup = markup[0]
	}

	body, err := json.Marshal(req)
	if err != nil {
		return 0, err
	}

	resp, err := c.HTTP.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	// Parse response to get message_id
	var result struct {
		Ok     bool `json:"ok"`
		Result struct {
			MessageID int `json:"message_id"`
		} `json:"result"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		// If decoding fails, it might be non-JSON error
		return 0, err
	}

	if !result.Ok {
		return 0, fmt.Errorf("telegram API error: %s", result.Description)
	}

	return result.Result.MessageID, nil
}

func (c *Client) AnswerCallbackQuery(callbackQueryID, text string) error {
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
	return nil
}
