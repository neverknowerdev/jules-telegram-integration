package firestore

import (
	"context"
)

type ClientInterface interface {
	SaveChatConfig(ctx context.Context, config ChatConfig) error
	GetChatConfig(ctx context.Context, chatID int64, threadID int) (*ChatConfig, error)
	GetChatsByChatID(ctx context.Context, chatID int64) ([]ChatConfig, error)
	DeleteChatConfig(ctx context.Context, chatID int64, threadID int) error
	UpdateCurrentSession(ctx context.Context, chatID int64, threadID int, sessionID string) error
	UpdateChatState(ctx context.Context, chatID int64, threadID int, state, draftSource string) error
	UpdateDraftBranch(ctx context.Context, chatID int64, threadID int, draftBranch string) error
	UpdateCreationMode(ctx context.Context, chatID int64, threadID int, mode string) error
	UpdateLastActivity(ctx context.Context, chatID int64, threadID int, activityID string) error
	UpdateProgressMessageID(ctx context.Context, chatID int64, threadID int, messageID int) error
	MarkPRAsNotified(ctx context.Context, chatID int64, threadID int, prURL string) error
	MarkBranchAsNotified(ctx context.Context, chatID int64, threadID int, branchName string) error
	IterateAllChats(ctx context.Context, fn func(ChatConfig) error) error
}
