package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

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
		firestoreClient, err = firestore.NewClient(ctx, projectID)
		if err != nil {
			log.Printf("Failed to create Firestore client: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	}

	chats, err := firestoreClient.GetAllChats(ctx)
	if err != nil {
		log.Printf("Failed to get chats: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	for _, chat := range chats {
		if chat.CurrentSession == "" {
			continue
		}

		activities, err := julesClient.ListActivities(chat.CurrentSession)
		if err != nil {
			log.Printf("Failed to list activities for chat %d: %v", chat.ChatID, err)
			continue
		}

		if len(activities) == 0 {
			continue
		}

		// Find new activities (activities are returned newest first usually)
		var newActivities []jules.Activity
		foundLast := false

		// If chat has no last activity recorded, we treat all as new (or limit them)
		// But to avoid spam on fresh start/switch, we might want to just mark the latest and return?
		// User requirement: "Receive every update".
		// If I switch to a task, I probably want to see its status.
		// Let's implement the "Top 5" heuristic if LastActivityID is not found.

		for _, act := range activities {
			if act.Id == chat.LastActivityID {
				foundLast = true
				break
			}
			newActivities = append(newActivities, act)
		}

		// If we found the last activity, we send all new ones.
		// If we DIDN'T find it, it implies either:
		// 1. It's a new session for the bot (LastActivityID is empty or from another session).
		// 2. The LastActivityID is too old and fell off the first page.
		// In case 2, we risk duplication if we send everything? No, if it's not in the list, then everything in the list is NEWER (assuming list is desc).
		// So actually we should send ALL.
		// EXCEPT if LastActivityID was empty (first run).

		if !foundLast && chat.LastActivityID == "" {
			// First run for this session. Just mark the latest and don't spam.
			// Or maybe send the very latest one as a "Status"?
			// Let's just update the cursor to the latest so we only get NEW updates from now on.
			if len(activities) > 0 {
				firestoreClient.UpdateLastActivity(ctx, chat.ChatID, activities[0].Id)
			}
			continue
		}

		// If not found but ID was set (switched session or gap), we send all (or limit to avoid spam).
		if !foundLast && len(newActivities) > 5 {
			// Limit to 5 to avoid spam
			newActivities = newActivities[:5]
		}

		if len(newActivities) == 0 {
			continue
		}

		// Reverse to send oldest first
		for i, j := 0, len(newActivities)-1; i < j; i, j = i+1, j-1 {
			newActivities[i], newActivities[j] = newActivities[j], newActivities[i]
		}

		for _, act := range newActivities {
			msg := formatActivity(act)
			telegramClient.SendMessage(chat.ChatID, msg)
		}

		// Update LastActivityID to the newest one processed
		if len(newActivities) > 0 {
			// The newest one is the LAST one in the reversed list?
			// No, the newest one is the FIRST one in the original list (activities[0]).
			// Or newActivities[len-1] after reverse.
			// Better: activities[0].Id is the absolute newest in the fetch.
			// But if we limited the list, we should update to the one we actually processed?
			// No, we should update to the absolute newest to avoid processing the skipped ones again.
			firestoreClient.UpdateLastActivity(ctx, chat.ChatID, activities[0].Id)
		}
	}

	w.WriteHeader(http.StatusOK)
}

func formatActivity(act jules.Activity) string {
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
		// We might want to skip user messages if we sent them?
		// But seeing them confirms receipt.
	} else if act.Originator == "agent" {
		title = "Agent Message"
	}

	return fmt.Sprintf("*%s*\n%s", title, desc)
}
