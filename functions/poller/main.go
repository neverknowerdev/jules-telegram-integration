package poller

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/neverknowerdev/jules-telegram-bot/internal/firestore"
	"github.com/neverknowerdev/jules-telegram-bot/internal/jules"
	"github.com/neverknowerdev/jules-telegram-bot/internal/telegram"
)

var (
	julesClient     *jules.Client
	firestoreClient *firestore.Client
	telegramClient  *telegram.Client
	projectID       string
)

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
			if act.PlanGenerated.Plan.Title != "" {
				sessionIDShort := strings.TrimPrefix(chat.CurrentSession, "sessions/")
				keyboard := telegram.InlineKeyboardMarkup{
					InlineKeyboard: [][]telegram.InlineKeyboardButton{
						{
							{Text: "✅ Approve Plan", CallbackData: "approve_plan:" + sessionIDShort},
						},
					},
				}
				telegramClient.SendMessageWithKeyboard(chat.ChatID, msg, keyboard)
			} else {
				telegramClient.SendMessage(chat.ChatID, msg)
			}
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
		title = "You"
		if act.UserMessaged.UserMessage != "" {
			desc = formatTelegramHTML(act.UserMessaged.UserMessage)
		}
	} else if act.Originator == "agent" {
		title = "Jules"
		if act.AgentMessaged.AgentMessage != "" {
			desc = formatTelegramHTML(act.AgentMessaged.AgentMessage)
		}
	}

	// Escape HTML for title safely
	title = strings.ReplaceAll(title, "&", "&amp;")
	title = strings.ReplaceAll(title, "<", "&lt;")
	title = strings.ReplaceAll(title, ">", "&gt;")

	if desc != "" {
		return fmt.Sprintf("🤖 <b>%s</b>\n%s", title, desc)
	}
	return fmt.Sprintf("🤖 <b>%s</b>", title)
}

func formatTelegramHTML(md string) string {
	// First escape <, >, & so they don't break HTML parsing
	md = strings.ReplaceAll(md, "&", "&amp;")
	md = strings.ReplaceAll(md, "<", "&lt;")
	md = strings.ReplaceAll(md, ">", "&gt;")

	// Markdown to HTML conversions
	// **bold**
	reBold := regexp.MustCompile(`(?s)\*\*(.*?)\*\*`)
	md = reBold.ReplaceAllString(md, "<b>$1</b>")

	// *italic*
	reItalic := regexp.MustCompile(`(?s)\*(.*?)\*`)
	md = reItalic.ReplaceAllString(md, "<i>$1</i>")

	// ```code block```
	reCodeBlock := regexp.MustCompile("(?s)```(.*?)```")
	md = reCodeBlock.ReplaceAllString(md, "<pre>$1</pre>")

	// `inline code`
	reInline := regexp.MustCompile("(?s)`(.*?)`")
	md = reInline.ReplaceAllString(md, "<code>$1</code>")

	return md
}
