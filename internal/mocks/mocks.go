package mocks

import (
	"context"
	"fmt"

	"github.com/neverknowerdev/jules-telegram-bot/internal/firestore"
	"github.com/neverknowerdev/jules-telegram-bot/internal/jules"
	"github.com/neverknowerdev/jules-telegram-bot/internal/telegram"
)

type MockJulesClient struct {
	Sources        []jules.Source
	Sessions       []jules.Session
	Activities     []jules.Activity
	CreatedSession *jules.Session
	SentMessages   []string
}

func (m *MockJulesClient) ListSources() ([]jules.Source, error) {
	return m.Sources, nil
}

func (m *MockJulesClient) ListSessions() ([]jules.Session, error) {
	return m.Sessions, nil
}

func (m *MockJulesClient) GetSession(sessionName string) (*jules.Session, error) {
	for _, s := range m.Sessions {
		if s.Name == sessionName {
			return &s, nil
		}
	}
	// Return a default mock session if not found in list, or handle dynamically
	return &jules.Session{Name: sessionName, Title: "Mocked Session", State: "IN_PROGRESS"}, nil
}

func (m *MockJulesClient) ListActivities(sessionName string, sinceID string) ([]jules.Activity, error) {
	return m.Activities, nil
}

func (m *MockJulesClient) ListAllActivities(sessionName string) ([]jules.Activity, error) {
	return m.Activities, nil
}

func (m *MockJulesClient) SendMessage(sessionName, message string) error {
	m.SentMessages = append(m.SentMessages, message)
	return nil
}

func (m *MockJulesClient) CreateSession(prompt, source, mode, branch string) (*jules.Session, error) {
	if m.CreatedSession != nil {
		return m.CreatedSession, nil
	}
	return &jules.Session{Name: "sessions/mock-created-session", Title: prompt, State: "IN_PROGRESS"}, nil
}

func (m *MockJulesClient) ArchiveSession(sessionName string) error {
	return nil
}

func (m *MockJulesClient) ApprovePlan(sessionName string) error {
	return nil
}

type MockTelegramClient struct {
	SentMessages          []string
	SentThreadIDs         []int
	SentKeyboards         []telegram.InlineKeyboardMarkup
	SentReplyKeyboards    []telegram.ReplyKeyboardMarkup
	DeletedTopics         []int
	CreatedTopics         []string
	AnsweredCallbackIDs   []string
	EditedMessages        []string
	SendMessageReturnID   int
	CreateTopicReturnID   int
}

func (m *MockTelegramClient) SendMessage(chatID int64, threadID int, text string) error {
	m.SentMessages = append(m.SentMessages, text)
	m.SentThreadIDs = append(m.SentThreadIDs, threadID)
	return nil
}

func (m *MockTelegramClient) SendMessageReturningID(chatID int64, threadID int, text string) (int, error) {
	m.SentMessages = append(m.SentMessages, text)
	m.SentThreadIDs = append(m.SentThreadIDs, threadID)
	return m.SendMessageReturnID, nil
}

func (m *MockTelegramClient) SendMessageWithKeyboard(chatID int64, threadID int, text string, keyboard telegram.InlineKeyboardMarkup) error {
	m.SentMessages = append(m.SentMessages, text)
	m.SentKeyboards = append(m.SentKeyboards, keyboard)
	return nil
}

func (m *MockTelegramClient) SendMessageWithKeyboardReturningID(chatID int64, threadID int, text string, keyboard telegram.InlineKeyboardMarkup) (int, error) {
	m.SentMessages = append(m.SentMessages, text)
	m.SentKeyboards = append(m.SentKeyboards, keyboard)
	return m.SendMessageReturnID, nil
}

func (m *MockTelegramClient) SendMessageWithReplyKeyboard(chatID int64, threadID int, text string, keyboard telegram.ReplyKeyboardMarkup) error {
	m.SentMessages = append(m.SentMessages, text)
	m.SentReplyKeyboards = append(m.SentReplyKeyboards, keyboard)
	return nil
}

func (m *MockTelegramClient) AnswerCallbackQuery(callbackQueryID string, text string) error {
	m.AnsweredCallbackIDs = append(m.AnsweredCallbackIDs, callbackQueryID)
	return nil
}

func (m *MockTelegramClient) EditMessageText(chatID int64, messageID int, text string, keyboard *telegram.InlineKeyboardMarkup) error {
	m.EditedMessages = append(m.EditedMessages, text)
	if keyboard != nil {
		m.SentKeyboards = append(m.SentKeyboards, *keyboard)
	}
	return nil
}

func (m *MockTelegramClient) SetWebhook(webhookURL string) error {
	return nil
}

func (m *MockTelegramClient) CreateForumTopic(chatID int64, name string) (int, error) {
	m.CreatedTopics = append(m.CreatedTopics, name)
	if m.CreateTopicReturnID != 0 {
		return m.CreateTopicReturnID, nil
	}
	return 999, nil // mock topic ID
}

func (m *MockTelegramClient) DeleteForumTopic(chatID int64, threadID int) error {
	m.DeletedTopics = append(m.DeletedTopics, threadID)
	return nil
}

func (m *MockTelegramClient) EditForumTopic(chatID int64, threadID int, name string) error {
	return nil
}

func (m *MockTelegramClient) PinChatMessage(chatID int64, threadID int, messageID int) error {
	return nil
}

func (m *MockTelegramClient) UnpinAllChatMessages(chatID int64, threadID int) error {
	return nil
}

type MockFirestoreClient struct {
	Configs        map[string]*firestore.ChatConfig
	Updates        []string
}

func NewMockFirestoreClient() *MockFirestoreClient {
	return &MockFirestoreClient{
		Configs: make(map[string]*firestore.ChatConfig),
	}
}

func (m *MockFirestoreClient) getDocID(chatID int64, threadID int) string {
	if threadID > 0 {
		return fmt.Sprintf("%d_%d", chatID, threadID)
	}
	return fmt.Sprintf("%d", chatID)
}

func (m *MockFirestoreClient) SaveChatConfig(ctx context.Context, config firestore.ChatConfig) error {
	docID := m.getDocID(config.ChatID, config.ThreadID)
	m.Configs[docID] = &config
	return nil
}

func (m *MockFirestoreClient) GetChatConfig(ctx context.Context, chatID int64, threadID int) (*firestore.ChatConfig, error) {
	docID := m.getDocID(chatID, threadID)
	if config, ok := m.Configs[docID]; ok {
		return config, nil
	}
	return nil, nil // Return empty config for test
}

func (m *MockFirestoreClient) GetChatsByChatID(ctx context.Context, chatID int64) ([]firestore.ChatConfig, error) {
	var chats []firestore.ChatConfig
	for _, config := range m.Configs {
		if config.ChatID == chatID {
			chats = append(chats, *config)
		}
	}
	return chats, nil
}

func (m *MockFirestoreClient) DeleteChatConfig(ctx context.Context, chatID int64, threadID int) error {
	docID := m.getDocID(chatID, threadID)
	delete(m.Configs, docID)
	return nil
}

func (m *MockFirestoreClient) UpdateCurrentSession(ctx context.Context, chatID int64, threadID int, sessionID string) error {
	docID := m.getDocID(chatID, threadID)
	if config, ok := m.Configs[docID]; ok {
		config.CurrentSession = sessionID
	}
	m.Updates = append(m.Updates, "UpdateCurrentSession")
	return nil
}

func (m *MockFirestoreClient) UpdateChatState(ctx context.Context, chatID int64, threadID int, state, draftSource string) error {
	docID := m.getDocID(chatID, threadID)
	if config, ok := m.Configs[docID]; ok {
		config.State = state
		config.DraftSource = draftSource
	}
	m.Updates = append(m.Updates, "UpdateChatState")
	return nil
}

func (m *MockFirestoreClient) UpdateDraftBranch(ctx context.Context, chatID int64, threadID int, draftBranch string) error {
	docID := m.getDocID(chatID, threadID)
	if config, ok := m.Configs[docID]; ok {
		config.DraftBranch = draftBranch
	}
	m.Updates = append(m.Updates, "UpdateDraftBranch")
	return nil
}

func (m *MockFirestoreClient) UpdateCreationMode(ctx context.Context, chatID int64, threadID int, mode string) error {
	docID := m.getDocID(chatID, threadID)
	if config, ok := m.Configs[docID]; ok {
		config.CreationMode = mode
	}
	m.Updates = append(m.Updates, "UpdateCreationMode")
	return nil
}

func (m *MockFirestoreClient) UpdateLastActivity(ctx context.Context, chatID int64, threadID int, activityID string) error {
	docID := m.getDocID(chatID, threadID)
	if config, ok := m.Configs[docID]; ok {
		config.LastActivityID = activityID
	}
	m.Updates = append(m.Updates, "UpdateLastActivity")
	return nil
}

func (m *MockFirestoreClient) UpdateProgressMessageID(ctx context.Context, chatID int64, threadID int, messageID int) error {
	docID := m.getDocID(chatID, threadID)
	if config, ok := m.Configs[docID]; ok {
		config.ProgressMessageID = messageID
	}
	m.Updates = append(m.Updates, "UpdateProgressMessageID")
	return nil
}

func (m *MockFirestoreClient) MarkPRAsNotified(ctx context.Context, chatID int64, threadID int, prURL string) error {
	m.Updates = append(m.Updates, "MarkPRAsNotified")
	return nil
}

func (m *MockFirestoreClient) MarkBranchAsNotified(ctx context.Context, chatID int64, threadID int, branchName string) error {
	m.Updates = append(m.Updates, "MarkBranchAsNotified")
	return nil
}

func (m *MockFirestoreClient) IterateAllChats(ctx context.Context, fn func(firestore.ChatConfig) error) error {
	for _, config := range m.Configs {
		if err := fn(*config); err != nil {
			return err
		}
	}
	return nil
}
