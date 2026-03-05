package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	MessageID         int                `json:"message_id"`
	MessageThreadID   int                `json:"message_thread_id,omitempty"`
	From              *User              `json:"from"`
	Chat              *Chat              `json:"chat"`
	IsTopicMessage    bool               `json:"is_topic_message,omitempty"`
	ForumTopicCreated *ForumTopicCreated `json:"forum_topic_created,omitempty"`
	Text              string             `json:"text"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    *User    `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
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
	CallbackData string `json:"callback_data,omitempty"`
	URL          string `json:"url,omitempty"`
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

func (c *Client) SendMessage(chatID int64, threadID int, text string) error {
	url := fmt.Sprintf(BaseURL+"/sendMessage", c.Token)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}
	if threadID > 0 {
		payload["message_thread_id"] = threadID
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
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("[TELEGRAM] SendMessage error: status=%d body=%s", resp.StatusCode, string(respBody))
		return fmt.Errorf("telegram API error: %d %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// SendMessageReturningID sends a message and returns the Telegram message_id.
func (c *Client) SendMessageReturningID(chatID int64, threadID int, text string) (int, error) {
	url := fmt.Sprintf(BaseURL+"/sendMessage", c.Token)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}
	if threadID > 0 {
		payload["message_thread_id"] = threadID
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	resp, err := c.HTTP.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Printf("[TELEGRAM] SendMessageReturningID error: status=%d body=%s", resp.StatusCode, string(respBody))
		return 0, fmt.Errorf("telegram API error: %d %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Result struct {
			MessageID int `json:"message_id"`
		} `json:"result"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return 0, err
	}
	return result.Result.MessageID, nil
}

func (c *Client) SendMessageWithKeyboardReturningID(chatID int64, threadID int, text string, keyboard InlineKeyboardMarkup) (int, error) {
	url := fmt.Sprintf(BaseURL+"/sendMessage", c.Token)

	payload := map[string]interface{}{
		"chat_id":      chatID,
		"text":         text,
		"parse_mode":   "HTML",
		"reply_markup": keyboard,
	}
	if threadID > 0 {
		payload["message_thread_id"] = threadID
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	resp, err := c.HTTP.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Printf("[TELEGRAM] SendMessageWithKeyboardReturningID error: status=%d body=%s", resp.StatusCode, string(respBody))
		return 0, fmt.Errorf("telegram API error: %d %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Result struct {
			MessageID int `json:"message_id"`
		} `json:"result"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return 0, err
	}
	return result.Result.MessageID, nil
}

func (c *Client) SendMessageWithKeyboard(chatID int64, threadID int, text string, keyboard InlineKeyboardMarkup) error {
	url := fmt.Sprintf(BaseURL+"/sendMessage", c.Token)

	payload := map[string]interface{}{
		"chat_id":      chatID,
		"text":         text,
		"parse_mode":   "HTML",
		"reply_markup": keyboard,
	}
	if threadID > 0 {
		payload["message_thread_id"] = threadID
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
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("[TELEGRAM] SendMessageWithKeyboard error: status=%d body=%s", resp.StatusCode, string(respBody))
		return fmt.Errorf("telegram API error: %d %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (c *Client) SendMessageWithReplyKeyboard(chatID int64, threadID int, text string, keyboard ReplyKeyboardMarkup) error {
	url := fmt.Sprintf(BaseURL+"/sendMessage", c.Token)

	payload := map[string]interface{}{
		"chat_id":      chatID,
		"text":         text,
		"parse_mode":   "HTML",
		"reply_markup": keyboard,
	}
	if threadID > 0 {
		payload["message_thread_id"] = threadID
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

func (c *Client) EditForumTopic(chatID int64, threadID int, name string) error {
	url := fmt.Sprintf(BaseURL+"/editForumTopic", c.Token)

	body, err := json.Marshal(map[string]interface{}{
		"chat_id":           chatID,
		"message_thread_id": threadID,
		"name":              name,
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
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("[TELEGRAM] EditForumTopic error: status=%d body=%s", resp.StatusCode, string(respBody))
		return fmt.Errorf("telegram API error: %d %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (c *Client) CreateForumTopic(chatID int64, name string) (int, error) {
	url := fmt.Sprintf(BaseURL+"/createForumTopic", c.Token)

	body, err := json.Marshal(map[string]interface{}{
		"chat_id": chatID,
		"name":    name,
	})
	if err != nil {
		return 0, err
	}

	resp, err := c.HTTP.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Printf("[TELEGRAM] CreateForumTopic error: status=%d body=%s", resp.StatusCode, string(respBody))
		return 0, fmt.Errorf("telegram API error: %d %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Result struct {
			MessageThreadID int `json:"message_thread_id"`
		} `json:"result"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return 0, err
	}
	return result.Result.MessageThreadID, nil
}

func (c *Client) DeleteForumTopic(chatID int64, threadID int) error {
	url := fmt.Sprintf(BaseURL+"/deleteForumTopic", c.Token)

	body, err := json.Marshal(map[string]interface{}{
		"chat_id":           chatID,
		"message_thread_id": threadID,
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
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("[TELEGRAM] DeleteForumTopic error: status=%d body=%s", resp.StatusCode, string(respBody))
		return fmt.Errorf("telegram API error: %d %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (c *Client) PinChatMessage(chatID int64, threadID int, messageID int) error {
	url := fmt.Sprintf(BaseURL+"/pinChatMessage", c.Token)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": messageID,
	}
	// Telegram's pinChatMessage does not strictly require message_thread_id,
	// but can optionally take it if needed depending on topic rules. Usually, the message_id is enough to pin in the topic.
	// But let's pass it just in case.
	// Actually, the API documentation for pinChatMessage says: "message_id" is required.
	// It doesn't use message_thread_id, because message_id is globally unique within the supergroup.

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
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("[TELEGRAM] PinChatMessage error: status=%d body=%s", resp.StatusCode, string(respBody))
		return fmt.Errorf("telegram API error: %d %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (c *Client) UnpinAllChatMessages(chatID int64, threadID int) error {
	url := fmt.Sprintf(BaseURL+"/unpinAllChatMessages", c.Token)

	payload := map[string]interface{}{
		"chat_id": chatID,
	}
	// unpinAllChatMessages also removes pinned messages from topics if message_thread_id is absent? No, wait.
	// The API doc: "unpinAllChatMessages removes all pinned messages. If chat is a forum, unpins all pinned messages in the specified topic."
	// We MUST pass message_thread_id if threadID is > 0.
	// But it says unpinAllForumTopicMessages is a separate method? No, unpinAllChatMessages takes chat_id.
	// Let's pass it. Wait, the API doc for unpinAllChatMessages does not mention message_thread_id.
	// Wait, actually it does in some versions. Let's look up unpinAllForumTopicMessages? It doesn't exist, it's just unpinAllChatMessages.
	if threadID > 0 {
		// Just unpinning from the chat if it's not a forum, or we might need to unpin all messages in general.
		// Wait, "unpinAllChatMessages" has a parameter: "chat_id". There is no "message_thread_id" for unpinAllChatMessages?
		// "unpinAllForumTopicMessages" is the actual method name. Let's check API. No, unpinAllChatMessages is universal.
		// Wait, "unpinAllChatMessages" clears the entire chat! We don't want to clear the whole supergroup.
		// There is "unpinAllForumTopicMessages" in telegram API. Let's use that if threadID > 0.
	}

	// Since we are unpinning inside a thread:
	var method string
	if threadID > 0 {
		method = "/unpinAllForumTopicMessages"
		payload["message_thread_id"] = threadID
	} else {
		method = "/unpinAllChatMessages"
	}

	url = fmt.Sprintf(BaseURL+method, c.Token)

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
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("[TELEGRAM] Unpin error (%s): status=%d body=%s", method, resp.StatusCode, string(respBody))
		return fmt.Errorf("telegram API error: %d %s", resp.StatusCode, string(respBody))
	}
	return nil
}

type ForumTopicCreated struct {
	Name              string `json:"name"`
	IconColor         int    `json:"icon_color"`
	IconCustomEmojiID string `json:"icon_custom_emoji_id,omitempty"`
}
