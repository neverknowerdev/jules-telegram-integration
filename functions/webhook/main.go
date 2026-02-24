package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/GoogleCloudPlatform/functions-framework-go/functions"
	"github.com/GoogleCloudPlatform/functions-framework-go/funcframework"
	"jules-telegram-bot/internal/firestore"
	"jules-telegram-bot/internal/jules"
	"jules-telegram-bot/internal/telegram"
)

var (
	julesClient     *jules.Client
	firestoreClient *firestore.Client
	telegramClient  *telegram.Client
	projectID       string
)

func init() {
	functions.HTTP("TelegramWebhook", TelegramWebhook)
}

func main() {
	initEnv()
	if err := funcframework.Start("8080"); err != nil {
		log.Fatalf("Function start error: %v", err)
	}
}

func initEnv() {
	projectID = os.Getenv("GCP_PROJECT")
	if projectID == "" {
		projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}

	apiKey := os.Getenv("JULES_API_KEY")
	telegramToken := os.Getenv("TELEGRAM_TOKEN")

	if apiKey != "" {
		julesClient = jules.NewClient(apiKey)
	}
	if telegramToken != "" {
		telegramClient = telegram.NewClient(telegramToken)
	}
}

func TelegramWebhook(w http.ResponseWriter, r *http.Request) {
	if projectID == "" {
		initEnv()
	}

	ctx := context.Background()

	if firestoreClient == nil {
		var err error
		if projectID == "" {
			log.Println("GCP_PROJECT or GOOGLE_CLOUD_PROJECT not set")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		firestoreClient, err = firestore.NewClient(ctx, projectID)
		if err != nil {
			log.Printf("Failed to create Firestore client: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	}

	var update telegram.Update
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		log.Printf("Failed to decode update: %v", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	if update.CallbackQuery != nil {
		handleCallback(ctx, update.CallbackQuery)
		w.WriteHeader(http.StatusOK)
		return
	}

	if update.Message == nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	chatID := update.Message.Chat.ID
	text := update.Message.Text

	if strings.HasPrefix(text, "/") {
		handleCommand(ctx, chatID, text)
	} else {
		handleMessage(ctx, update.Message)
	}

	w.WriteHeader(http.StatusOK)
}

func handleCallback(ctx context.Context, callback *telegram.CallbackQuery) {
	data := callback.Data
	if callback.Message == nil || callback.Message.Chat == nil {
		return
	}
	chatID := callback.Message.Chat.ID

	if strings.HasPrefix(data, "switch:") {
		sessionID := strings.TrimPrefix(data, "switch:")
		if err := firestoreClient.UpdateCurrentSession(ctx, chatID, sessionID); err != nil {
			log.Printf("Failed to switch session: %v", err)
			telegramClient.AnswerCallbackQuery(callback.ID, "Failed to switch.")
			return
		}

		telegramClient.AnswerCallbackQuery(callback.ID, "Switched!")
		telegramClient.SendMessage(chatID, fmt.Sprintf("Context switched to: %s", sessionID))
	}
}

func handleCommand(ctx context.Context, chatID int64, text string) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return
	}
	command := parts[0]

	if firestoreClient == nil || julesClient == nil || telegramClient == nil {
		log.Println("Clients not initialized")
		return
	}

	switch command {
	case "/start":
		config := firestore.ChatConfig{
			ChatID: chatID,
		}
		if err := firestoreClient.SaveChatConfig(ctx, config); err != nil {
			log.Printf("Failed to save chat config: %v", err)
			telegramClient.SendMessage(chatID, "Failed to register chat.")
			return
		}
		telegramClient.SendMessage(chatID, "Welcome! Use /tasks to see active sessions from all your repositories.")

	case "/tasks":
		sessions, err := julesClient.ListSessions()
		if err != nil {
			log.Printf("Failed to list sessions: %v", err)
			telegramClient.SendMessage(chatID, "Failed to list sessions.")
			return
		}

		grouped := make(map[string][]jules.Session)
		for _, s := range sessions {
			repo := s.SourceContext.Source
			grouped[repo] = append(grouped[repo], s)
		}

		if len(sessions) == 0 {
			telegramClient.SendMessage(chatID, "No active sessions found.")
			return
		}

		chatConfig, _ := firestoreClient.GetChatConfig(ctx, chatID)
		currentSession := ""
		if chatConfig != nil {
			currentSession = chatConfig.CurrentSession
		}

		var repos []string
		for r := range grouped {
			repos = append(repos, r)
		}
		sort.Strings(repos)

		var rows [][]telegram.InlineKeyboardButton

		for _, repo := range repos {
			repoName := repo
			parts := strings.Split(repo, "/")
			if len(parts) >= 4 {
				repoName = fmt.Sprintf("%s/%s", parts[2], parts[3])
			}

			for _, s := range grouped[repo] {
				marker := ""
				if currentSession == s.Name {
					marker = " ✅"
				}

				btnText := fmt.Sprintf("[%s] %s%s", repoName, s.Title, marker)
				// Truncate
				runes := []rune(btnText)
				if len(runes) > 40 {
					btnText = string(runes[:37]) + "..."
				}

				rows = append(rows, []telegram.InlineKeyboardButton{
					{Text: btnText, CallbackData: "switch:" + s.Name},
				})
			}
		}

		markup := telegram.InlineKeyboardMarkup{InlineKeyboard: rows}
		telegramClient.SendMessage(chatID, "Select a session to switch context:", markup)

	case "/status":
		chatConfig, err := firestoreClient.GetChatConfig(ctx, chatID)
		if err != nil {
			telegramClient.SendMessage(chatID, "Could not retrieve chat status.")
			return
		}
		if chatConfig.CurrentSession == "" {
			telegramClient.SendMessage(chatID, "No session currently selected.")
			return
		}

		telegramClient.SendMessage(chatID, fmt.Sprintf("Current Session: %s", chatConfig.CurrentSession))

	case "/switch":
		if len(parts) < 2 {
			telegramClient.SendMessage(chatID, "Usage: /switch <session_id>")
			return
		}
		sessionID := parts[1]
		if !strings.HasPrefix(sessionID, "sessions/") {
			sessionID = "sessions/" + sessionID
		}

		if err := firestoreClient.UpdateCurrentSession(ctx, chatID, sessionID); err != nil {
			log.Printf("Failed to switch session: %v", err)
			telegramClient.SendMessage(chatID, "Failed to switch session.")
			return
		}
		telegramClient.SendMessage(chatID, fmt.Sprintf("Switched to session: %s", sessionID))

	default:
		telegramClient.SendMessage(chatID, "Unknown command.")
	}
}

func handleMessage(ctx context.Context, msg *telegram.Message) {
	if firestoreClient == nil || julesClient == nil || telegramClient == nil {
		log.Println("Clients not initialized")
		return
	}

	targetSession := ""

	// Check if reply
	if msg.ReplyToMessage != nil {
		sessionID, err := firestoreClient.GetSessionForMessage(ctx, msg.Chat.ID, msg.ReplyToMessage.MessageID)
		if err == nil && sessionID != "" {
			targetSession = sessionID
		} else {
			log.Printf("Could not find session for reply message %d", msg.ReplyToMessage.MessageID)
		}
	}

	if targetSession == "" {
		chatConfig, err := firestoreClient.GetChatConfig(ctx, msg.Chat.ID)
		if err != nil || chatConfig.CurrentSession == "" {
			telegramClient.SendMessage(msg.Chat.ID, "No active session selected. Use /tasks to select one or reply to a specific session message.")
			return
		}
		targetSession = chatConfig.CurrentSession
	}

	if err := julesClient.SendMessage(targetSession, msg.Text); err != nil {
		log.Printf("Failed to send message to Jules: %v", err)
		telegramClient.SendMessage(msg.Chat.ID, "Failed to send message to Jules.")
		return
	}
}
