package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
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
	selectedSource  string
)

func init() {
	functions.HTTP("TelegramWebhook", TelegramWebhook)
}

func main() {
	// Initialize environment and clients for local execution
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
	selectedSource = os.Getenv("SELECTED_SOURCE")

	if apiKey != "" {
		julesClient = jules.NewClient(apiKey)
	}
	if telegramToken != "" {
		telegramClient = telegram.NewClient(telegramToken)
	}
}

func TelegramWebhook(w http.ResponseWriter, r *http.Request) {
	// Ensure env is init if cold start
	if projectID == "" {
		initEnv()
	}

	ctx := context.Background()

	// Lazy init Firestore
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

	if update.Message == nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	chatID := update.Message.Chat.ID
	text := update.Message.Text

	if strings.HasPrefix(text, "/") {
		handleCommand(ctx, chatID, text)
	} else {
		handleMessage(ctx, chatID, text)
	}

	w.WriteHeader(http.StatusOK)
}

func handleCommand(ctx context.Context, chatID int64, text string) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return
	}
	command := parts[0]

	// Ensure clients are ready
	if firestoreClient == nil || julesClient == nil || telegramClient == nil {
		log.Println("Clients not initialized")
		return
	}

	switch command {
	case "/start":
		config := firestore.ChatConfig{
			ChatID: chatID,
			Source: selectedSource,
		}
		if err := firestoreClient.SaveChatConfig(ctx, config); err != nil {
			log.Printf("Failed to save chat config: %v", err)
			telegramClient.SendMessage(chatID, "Failed to register chat.")
			return
		}
		msg := fmt.Sprintf("Welcome! I am bound to repository: %s.\nUse /tasks to see active sessions.", selectedSource)
		telegramClient.SendMessage(chatID, msg)

	case "/tasks":
		sessions, err := julesClient.ListSessions()
		if err != nil {
			log.Printf("Failed to list sessions: %v", err)
			telegramClient.SendMessage(chatID, "Failed to list sessions.")
			return
		}

		// Filter sessions by source if selectedSource is set
		// Jules session objects have SourceContext.Source

		var msg strings.Builder
		msg.WriteString("Active Sessions:\n")
		count := 0

		chatConfig, _ := firestoreClient.GetChatConfig(ctx, chatID)
		currentSession := ""
		if chatConfig != nil {
			currentSession = chatConfig.CurrentSession
		}

		for _, s := range sessions {
			// Check if source matches
			// source string format: "sources/github/owner/repo" or similar.
			// selectedSource should match s.SourceContext.Source
			if selectedSource != "" && s.SourceContext.Source != selectedSource {
				continue
			}

			marker := ""
			if currentSession == s.Name {
				marker = " (current)"
			}

			msg.WriteString(fmt.Sprintf("- %s: %s%s\n/switch %s\n", s.Title, s.Name, marker, s.Name))
			count++
		}

		if count == 0 {
			telegramClient.SendMessage(chatID, "No active sessions found for this source.")
		} else {
			telegramClient.SendMessage(chatID, msg.String())
		}

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
		// Handle potential missing prefix if user just copy-pasted partial ID
		if !strings.HasPrefix(sessionID, "sessions/") {
			// Only prepend if it looks like a raw ID (digits)
			// But safer to assume user pastes full name from /tasks list
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

func handleMessage(ctx context.Context, chatID int64, text string) {
	if firestoreClient == nil || julesClient == nil || telegramClient == nil {
		log.Println("Clients not initialized")
		return
	}

	chatConfig, err := firestoreClient.GetChatConfig(ctx, chatID)
	if err != nil || chatConfig.CurrentSession == "" {
		telegramClient.SendMessage(chatID, "No active session. Use /tasks to select one.")
		return
	}

	if err := julesClient.SendMessage(chatConfig.CurrentSession, text); err != nil {
		log.Printf("Failed to send message to Jules: %v", err)
		telegramClient.SendMessage(chatID, "Failed to send message to Jules.")
		return
	}
}
