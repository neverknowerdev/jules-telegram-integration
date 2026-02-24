package main

import (
	"context"
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
)

func init() {
	functions.HTTP("JulesPoller", JulesPoller)
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

func JulesPoller(w http.ResponseWriter, r *http.Request) {
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

	sessions, err := julesClient.ListSessions()
	if err != nil {
		log.Printf("Failed to list sessions: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	chats, err := firestoreClient.GetAllChats(ctx)
	if err != nil {
		log.Printf("Failed to get chats: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	for _, session := range sessions {
		activities, err := julesClient.ListActivities(session.Name)
		if err != nil {
			log.Printf("Failed to list activities for session %s: %v", session.Name, err)
			continue
		}

		if len(activities) == 0 {
			continue
		}

		for _, chat := range chats {
			lastID := ""
			if chat.SessionCursors != nil {
				lastID = chat.SessionCursors[session.Name]
			}

			var newActivities []jules.Activity
			foundLast := false

			for _, act := range activities {
				if act.Id == lastID {
					foundLast = true
					break
				}
				newActivities = append(newActivities, act)
			}

			if !foundLast && lastID == "" {
				if len(activities) > 0 {
					firestoreClient.UpdateSessionCursor(ctx, chat.ChatID, session.Name, activities[0].Id)
				}
				continue
			}

			if !foundLast && len(newActivities) > 5 {
				newActivities = newActivities[:5]
			}

			if len(newActivities) == 0 {
				continue
			}

			// Reverse
			for i, j := 0, len(newActivities)-1; i < j; i, j = i+1, j-1 {
				newActivities[i], newActivities[j] = newActivities[j], newActivities[i]
			}

			latestSentID := ""

			for _, act := range newActivities {
				formattedMsg := formatActivity(session, act)
				escapedMsg := telegram.EscapeMarkdown(formattedMsg)

				msgID, err := telegramClient.SendMessage(chat.ChatID, escapedMsg)
				if err != nil {
					log.Printf("Failed to send message to chat %d: %v", chat.ChatID, err)
					continue
				}

				if msgID != 0 {
					firestoreClient.SaveMessageContext(ctx, chat.ChatID, msgID, session.Name)
				}
				latestSentID = act.Id
			}

			if latestSentID != "" {
				firestoreClient.UpdateSessionCursor(ctx, chat.ChatID, session.Name, latestSentID)
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}

func formatActivity(session jules.Session, act jules.Activity) string {
	repo := session.SourceContext.Source
	parts := strings.Split(repo, "/")
	if len(parts) >= 4 {
		repo = fmt.Sprintf("%s/%s", parts[2], parts[3])
	} else if strings.HasPrefix(repo, "sources/") {
		repo = strings.TrimPrefix(repo, "sources/")
	}

	title := "New Activity"
	desc := ""

	if act.ProgressUpdated.Title != "" {
		title = act.ProgressUpdated.Title
		desc = act.ProgressUpdated.Description
	} else if act.PlanGenerated.Plan.Title != "" {
		title = "Plan Generated"
		desc = act.PlanGenerated.Plan.Title
	} else if act.Originator == "user" {
		title = "User Message"
	} else if act.Originator == "agent" {
		title = "Agent Message"
	}

	return fmt.Sprintf("[%s] %s\n*%s*\n%s", repo, session.Title, title, desc)
}
