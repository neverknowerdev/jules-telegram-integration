package telegram

type ClientInterface interface {
	SendMessage(chatID int64, threadID int, text string) error
	SendMessageReturningID(chatID int64, threadID int, text string) (int, error)
	SendMessageWithKeyboard(chatID int64, threadID int, text string, keyboard InlineKeyboardMarkup) error
	SendMessageWithKeyboardReturningID(chatID int64, threadID int, text string, keyboard InlineKeyboardMarkup) (int, error)
	SendMessageWithReplyKeyboard(chatID int64, threadID int, text string, keyboard ReplyKeyboardMarkup) error
	AnswerCallbackQuery(callbackQueryID string, text string) error
	EditMessageText(chatID int64, messageID int, text string, keyboard *InlineKeyboardMarkup) error
	SetWebhook(webhookURL string) error
	CreateForumTopic(chatID int64, name string) (int, error)
	DeleteForumTopic(chatID int64, threadID int) error
	EditForumTopic(chatID int64, threadID int, name string) error
	PinChatMessage(chatID int64, threadID int, messageID int) error
	UnpinAllChatMessages(chatID int64, threadID int) error
}
