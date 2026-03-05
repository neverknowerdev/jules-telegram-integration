package firestore

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

type ChatConfig struct {
	ChatID            int64           `firestore:"chat_id"`
	ThreadID          int             `firestore:"thread_id"`
	Source            string          `firestore:"source"`
	CurrentSession    string          `firestore:"current_session"`
	LastActivityID    string          `firestore:"last_activity_id"`
	State             string          `firestore:"state"`
	DraftSource       string          `firestore:"draft_source"`
	CreationMode      string          `firestore:"creation_mode"`
	ProgressMessageID int             `firestore:"progress_message_id"`
	NotifiedPRs       map[string]bool `firestore:"notified_prs"`
	NotifiedBranches  map[string]bool `firestore:"notified_branches"`
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

func (c *Client) getDocID(chatID int64, threadID int) string {
	if threadID > 0 {
		return fmt.Sprintf("%d_%d", chatID, threadID)
	}
	return fmt.Sprintf("%d", chatID)
}

func (c *Client) SaveChatConfig(ctx context.Context, config ChatConfig) error {
	docID := c.getDocID(config.ChatID, config.ThreadID)
	_, err := c.client.Collection("chats").Doc(docID).Set(ctx, config)
	return err
}

func (c *Client) GetChatConfig(ctx context.Context, chatID int64, threadID int) (*ChatConfig, error) {
	docID := c.getDocID(chatID, threadID)
	doc, err := c.client.Collection("chats").Doc(docID).Get(ctx)
	if err != nil {
		return nil, err
	}
	var config ChatConfig
	if err := doc.DataTo(&config); err != nil {
		return nil, err
	}
	return &config, nil
}

func (c *Client) DeleteChatConfig(ctx context.Context, chatID int64, threadID int) error {
	docID := c.getDocID(chatID, threadID)
	_, err := c.client.Collection("chats").Doc(docID).Delete(ctx)
	return err
}

func (c *Client) UpdateCurrentSession(ctx context.Context, chatID int64, threadID int, sessionID string) error {
	docID := c.getDocID(chatID, threadID)
	_, err := c.client.Collection("chats").Doc(docID).Update(ctx, []firestore.Update{
		{Path: "current_session", Value: sessionID},
	})
	return err
}

func (c *Client) UpdateChatState(ctx context.Context, chatID int64, threadID int, state, draftSource string) error {
	docID := c.getDocID(chatID, threadID)
	_, err := c.client.Collection("chats").Doc(docID).Update(ctx, []firestore.Update{
		{Path: "state", Value: state},
		{Path: "draft_source", Value: draftSource},
	})
	return err
}

func (c *Client) UpdateCreationMode(ctx context.Context, chatID int64, threadID int, mode string) error {
	docID := c.getDocID(chatID, threadID)
	_, err := c.client.Collection("chats").Doc(docID).Update(ctx, []firestore.Update{
		{Path: "creation_mode", Value: mode},
	})
	return err
}

func (c *Client) UpdateLastActivity(ctx context.Context, chatID int64, threadID int, activityID string) error {
	docID := c.getDocID(chatID, threadID)
	_, err := c.client.Collection("chats").Doc(docID).Update(ctx, []firestore.Update{
		{Path: "last_activity_id", Value: activityID},
	})
	return err
}

func (c *Client) UpdateProgressMessageID(ctx context.Context, chatID int64, threadID int, messageID int) error {
	docID := c.getDocID(chatID, threadID)
	_, err := c.client.Collection("chats").Doc(docID).Update(ctx, []firestore.Update{
		{Path: "progress_message_id", Value: messageID},
	})
	return err
}

func (c *Client) MarkPRAsNotified(ctx context.Context, chatID int64, threadID int, prURL string) error {
	docID := c.getDocID(chatID, threadID)
	_, err := c.client.Collection("chats").Doc(docID).Update(ctx, []firestore.Update{
		{FieldPath: firestore.FieldPath{"notified_prs", prURL}, Value: true},
	})
	return err
}

func (c *Client) MarkBranchAsNotified(ctx context.Context, chatID int64, threadID int, branchName string) error {
	docID := c.getDocID(chatID, threadID)
	_, err := c.client.Collection("chats").Doc(docID).Update(ctx, []firestore.Update{
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
		if err := fn(chat); err != nil {
			// Log error but continue iterating other chats
			fmt.Printf("[FIRESTORE] Error in chat callback: %v\n", err)
		}
	}
	return nil
}
