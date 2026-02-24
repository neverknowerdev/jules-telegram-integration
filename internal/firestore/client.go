package firestore

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ChatConfig struct {
	ChatID         int64             `firestore:"chat_id"`
	CurrentSession string            `firestore:"current_session"`
	SessionCursors map[string]string `firestore:"session_cursors"`
}

type MessageContext struct {
	ChatID    int64  `firestore:"chat_id"`
	MessageID int    `firestore:"message_id"`
	SessionID string `firestore:"session_id"`
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
	_, err := c.client.Collection("chats").Doc(fmt.Sprintf("%d", config.ChatID)).Set(ctx, config, firestore.MergeAll)
	return err
}

func (c *Client) GetChatConfig(ctx context.Context, chatID int64) (*ChatConfig, error) {
	doc, err := c.client.Collection("chats").Doc(fmt.Sprintf("%d", chatID)).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return &ChatConfig{
				ChatID:         chatID,
				SessionCursors: make(map[string]string),
			}, nil
		}
		return nil, err
	}
	var config ChatConfig
	if err := doc.DataTo(&config); err != nil {
		return nil, err
	}
	if config.SessionCursors == nil {
		config.SessionCursors = make(map[string]string)
	}
	return &config, nil
}

func (c *Client) UpdateCurrentSession(ctx context.Context, chatID int64, sessionID string) error {
	_, err := c.client.Collection("chats").Doc(fmt.Sprintf("%d", chatID)).Set(ctx, map[string]interface{}{
		"current_session": sessionID,
		"chat_id":         chatID,
	}, firestore.MergeAll)
	return err
}

func (c *Client) UpdateSessionCursor(ctx context.Context, chatID int64, sessionID, activityID string) error {
	ref := c.client.Collection("chats").Doc(fmt.Sprintf("%d", chatID))

	// Transaction to ensure we don't wipe existing cursors if map is large?
	// Actually, MergeAll handles partial map updates if key is "session_cursors.sessionID"
	// But dot notation keys in map require special handling in Set?
	// It's safer to read-modify-write.

	return c.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(ref)
		var config ChatConfig
		if err != nil {
			if status.Code(err) == codes.NotFound {
				config = ChatConfig{
					ChatID:         chatID,
					SessionCursors: make(map[string]string),
				}
			} else {
				return err
			}
		} else {
			if err := doc.DataTo(&config); err != nil {
				return err
			}
		}

		if config.SessionCursors == nil {
			config.SessionCursors = make(map[string]string)
		}

		config.SessionCursors[sessionID] = activityID

		return tx.Set(ref, config)
	})
}

func (c *Client) SaveMessageContext(ctx context.Context, chatID int64, messageID int, sessionID string) error {
	msgCtx := MessageContext{
		ChatID:    chatID,
		MessageID: messageID,
		SessionID: sessionID,
	}
	id := fmt.Sprintf("%d_%d", chatID, messageID)
	_, err := c.client.Collection("message_contexts").Doc(id).Set(ctx, msgCtx)
	return err
}

func (c *Client) GetSessionForMessage(ctx context.Context, chatID int64, messageID int) (string, error) {
	id := fmt.Sprintf("%d_%d", chatID, messageID)
	doc, err := c.client.Collection("message_contexts").Doc(id).Get(ctx)
	if err != nil {
		return "", err
	}
	var msgCtx MessageContext
	if err := doc.DataTo(&msgCtx); err != nil {
		return "", err
	}
	return msgCtx.SessionID, nil
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
		if chat.SessionCursors == nil {
			chat.SessionCursors = make(map[string]string)
		}
		chats = append(chats, chat)
	}
	return chats, nil
}
