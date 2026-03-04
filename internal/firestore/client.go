package firestore

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

type ChatConfig struct {
	ChatID         int64  `firestore:"chat_id"`
	Source         string `firestore:"source"`
	CurrentSession string `firestore:"current_session"`
	LastActivityID string `firestore:"last_activity_id"`
	State          string `firestore:"state"`
	DraftSource    string `firestore:"draft_source"`
	CreationMode   string `firestore:"creation_mode"`
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
	return &config, nil
}

func (c *Client) UpdateCurrentSession(ctx context.Context, chatID int64, sessionID string) error {
	_, err := c.client.Collection("chats").Doc(fmt.Sprintf("%d", chatID)).Update(ctx, []firestore.Update{
		{Path: "current_session", Value: sessionID},
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

func (c *Client) GetAllChats(ctx context.Context) ([]ChatConfig, error) {
	var chats []ChatConfig
	iter := c.client.Collection("chats").Documents(ctx)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var chat ChatConfig
		if err := doc.DataTo(&chat); err != nil {
			return nil, err
		}
		chats = append(chats, chat)
	}
	return chats, nil
}
