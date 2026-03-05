package firestore

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

type ChatConfig struct {
	ChatID              int64           `firestore:"chat_id"`
	Source              string          `firestore:"source"`
	CurrentSession      string          `firestore:"current_session"`
	LastActivityID      string          `firestore:"last_activity_id"`
	State               string          `firestore:"state"`
	DraftSource         string          `firestore:"draft_source"`
	CreationMode        string          `firestore:"creation_mode"`
	InteractiveInterval int             `firestore:"interactive_interval"` // in seconds
	StandardInterval    int             `firestore:"standard_interval"`    // in minutes
	IsWaitingForJules   bool            `firestore:"is_waiting_for_jules"`
	LastStandardPoll    int64           `firestore:"last_standard_poll"`
	ProgressMessageID   int             `firestore:"progress_message_id"`
	NotifiedPRs         map[string]bool `firestore:"notified_prs"`
	NotifiedBranches    map[string]bool `firestore:"notified_branches"`
}

type Client struct {
	client *firestore.Client
}

func NewClient(ctx context.Context, projectID string) (*Client, error) {
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return &Client{client: client}, nil
}

func (c *Client) Close() error {
	return c.client.Close()
}

func (c *Client) SaveChatConfig(ctx context.Context, config ChatConfig) error {
	_, err := c.client.Collection("chats").Doc(fmt.Sprintf("%d", config.ChatID)).Set(ctx, config)
	return err
}

func (c *Client) GetChatConfig(ctx context.Context, chatID int64) (*ChatConfig, error) {
	doc, err := c.client.Collection("chats").Doc(fmt.Sprintf("%d", chatID)).Get(ctx)
	if err != nil {
		return nil, err
	}
	var config ChatConfig
	if err := doc.DataTo(&config); err != nil {
		return nil, err
	}

	// Apply defaults
	if config.InteractiveInterval <= 0 {
		config.InteractiveInterval = 5
	}
	if config.StandardInterval <= 0 {
		config.StandardInterval = 5
	}

	return &config, nil
}

func (c *Client) UpdateCurrentSession(ctx context.Context, chatID int64, sessionID string) error {
	_, err := c.client.Collection("chats").Doc(fmt.Sprintf("%d", chatID)).Update(ctx, []firestore.Update{
		{Path: "current_session", Value: sessionID},
	})
	return err
}

func (c *Client) UpdateLastStandardPoll(ctx context.Context, chatID int64, timestamp int64) error {
	_, err := c.client.Collection("chats").Doc(fmt.Sprintf("%d", chatID)).Update(ctx, []firestore.Update{
		{Path: "last_standard_poll", Value: timestamp},
	})
	return err
}

func (c *Client) UpdateIsWaitingForJules(ctx context.Context, chatID int64, isWaiting bool) error {
	_, err := c.client.Collection("chats").Doc(fmt.Sprintf("%d", chatID)).Update(ctx, []firestore.Update{
		{Path: "is_waiting_for_jules", Value: isWaiting},
	})
	return err
}

func (c *Client) UpdateIntervals(ctx context.Context, chatID int64, interactive, standard int) error {
	_, err := c.client.Collection("chats").Doc(fmt.Sprintf("%d", chatID)).Update(ctx, []firestore.Update{
		{Path: "interactive_interval", Value: interactive},
		{Path: "standard_interval", Value: standard},
	})
	return err
}

func (c *Client) UpdateChatState(ctx context.Context, chatID int64, state, draftSource string) error {
	_, err := c.client.Collection("chats").Doc(fmt.Sprintf("%d", chatID)).Update(ctx, []firestore.Update{
		{Path: "state", Value: state},
		{Path: "draft_source", Value: draftSource},
	})
	return err
}

func (c *Client) UpdateCreationMode(ctx context.Context, chatID int64, mode string) error {
	_, err := c.client.Collection("chats").Doc(fmt.Sprintf("%d", chatID)).Update(ctx, []firestore.Update{
		{Path: "creation_mode", Value: mode},
	})
	return err
}

func (c *Client) UpdateLastActivity(ctx context.Context, chatID int64, activityID string) error {
	_, err := c.client.Collection("chats").Doc(fmt.Sprintf("%d", chatID)).Update(ctx, []firestore.Update{
		{Path: "last_activity_id", Value: activityID},
	})
	return err
}

func (c *Client) UpdateProgressMessageID(ctx context.Context, chatID int64, messageID int) error {
	_, err := c.client.Collection("chats").Doc(fmt.Sprintf("%d", chatID)).Update(ctx, []firestore.Update{
		{Path: "progress_message_id", Value: messageID},
	})
	return err
}

func (c *Client) MarkPRAsNotified(ctx context.Context, chatID int64, prURL string) error {
	_, err := c.client.Collection("chats").Doc(fmt.Sprintf("%d", chatID)).Update(ctx, []firestore.Update{
		{FieldPath: firestore.FieldPath{"notified_prs", prURL}, Value: true},
	})
	return err
}

func (c *Client) MarkBranchAsNotified(ctx context.Context, chatID int64, branchName string) error {
	_, err := c.client.Collection("chats").Doc(fmt.Sprintf("%d", chatID)).Update(ctx, []firestore.Update{
		{FieldPath: firestore.FieldPath{"notified_branches", branchName}, Value: true},
	})
	return err
}

func (c *Client) IterateAllChats(ctx context.Context, fn func(ChatConfig) error) error {
	iter := c.client.Collection("chats").Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
		var chat ChatConfig
		if err := doc.DataTo(&chat); err != nil {
			return err
		}
		// Apply defaults
		if chat.InteractiveInterval <= 0 {
			chat.InteractiveInterval = 5
		}
		if chat.StandardInterval <= 0 {
			chat.StandardInterval = 5
		}
		if err := fn(chat); err != nil {
			// Log error but continue iterating other chats
			fmt.Printf("[FIRESTORE] Error in chat callback: %v\n", err)
		}
	}
	return nil
}

type PollerState struct {
	LastHeartbeat int64 `firestore:"last_heartbeat"`
}

func (c *Client) GetPollerState(ctx context.Context) (*PollerState, error) {
	doc, err := c.client.Collection("global").Doc("poller_state").Get(ctx)
	if err != nil {
		return nil, err
	}
	var state PollerState
	if err := doc.DataTo(&state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (c *Client) UpdatePollerHeartbeat(ctx context.Context, timestamp int64) error {
	_, err := c.client.Collection("global").Doc("poller_state").Set(ctx, map[string]interface{}{
		"last_heartbeat": timestamp,
	}, firestore.MergeAll)
	return err
}
