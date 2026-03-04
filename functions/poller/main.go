package poller

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

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

	// Check poller lock
	pollerState, err := firestoreClient.GetPollerState(ctx)
	now := time.Now().Unix()
	if err == nil && pollerState != nil {
		// If another instance updated the heartbeat within the last 15 seconds, exit
		if now-pollerState.LastHeartbeat < 15 {
			log.Println("Another long-running poller instance is active. Exiting.")
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	// Calculate a deadline for the function to stop before Cloud Functions timeout (e.g. 55 mins)
	deadline := time.Now().Add(55 * time.Minute)

	for {
		if time.Now().After(deadline) {
			log.Println("Approaching function timeout, exiting loop.")
			break
		}

		// Update heartbeat
		firestoreClient.UpdatePollerHeartbeat(ctx, time.Now().Unix())

		chats, err := firestoreClient.GetAllChats(ctx)
		if err != nil {
			log.Printf("Failed to get chats: %v", err)
			break
		}

		minInteractiveInterval := 999999

		for _, chat := range chats {
			if chat.CurrentSession == "" {
				continue
			}

			if chat.IsWaitingForJules {
				if chat.InteractiveInterval > 0 && chat.InteractiveInterval < minInteractiveInterval {
					minInteractiveInterval = chat.InteractiveInterval
				}
			} else {
				// Throttle standard polling
				sinceLastPoll := time.Now().Unix() - chat.LastStandardPoll
				standardIntervalSecs := int64(chat.StandardInterval) * 60
				if chat.LastStandardPoll > 0 && sinceLastPoll < standardIntervalSecs {
					// Skip this chat until its standard interval has passed
					continue
				}
			}

			activities, err := julesClient.ListActivities(chat.CurrentSession)
			if err != nil {
				log.Printf("Failed to list activities for chat %d: %v", chat.ChatID, err)
				continue
			}

			if !chat.IsWaitingForJules {
				firestoreClient.UpdateLastStandardPoll(ctx, chat.ChatID, time.Now().Unix())
			}

			if len(activities) == 0 {
				continue
			}

			absoluteLatest := activities[0]
			julesWorking := false

			// Determine if the task is currently active/processing based on the absolute latest activity
			if absoluteLatest.Originator == "user" {
				julesWorking = true
			} else if absoluteLatest.Originator == "agent" || absoluteLatest.Originator == "system" {
				if absoluteLatest.ProgressUpdated.Title != "" {
					julesWorking = true
				}
			}

			if chat.IsWaitingForJules && !julesWorking {
				// Jules finished. Reset flag to false immediately.
				firestoreClient.UpdateIsWaitingForJules(ctx, chat.ChatID, false)
				chat.IsWaitingForJules = false
			} else if !chat.IsWaitingForJules && julesWorking {
				// We detected an active task during standard polling. Switch to interactive mode.
				firestoreClient.UpdateIsWaitingForJules(ctx, chat.ChatID, true)
				chat.IsWaitingForJules = true
				if chat.InteractiveInterval > 0 && chat.InteractiveInterval < minInteractiveInterval {
					minInteractiveInterval = chat.InteractiveInterval
				}
			}

			var newActivities []jules.Activity
			foundLast := false

			for _, act := range activities {
				if act.Id == chat.LastActivityID {
					foundLast = true
					break
				}
				newActivities = append(newActivities, act)
			}

			if !foundLast && chat.LastActivityID == "" {
				if len(activities) > 0 {
					firestoreClient.UpdateLastActivity(ctx, chat.ChatID, activities[0].Id)
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

			julesFinished := false

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

				// Check if Jules is done
				if act.Originator == "agent" || act.Originator == "system" {
					if act.ProgressUpdated.Title == "" {
						// It's not just a progress update, so Jules likely finished (PlanGenerated or AgentMessaged)
						julesFinished = true
					}
				}
			}

			if len(newActivities) > 0 {
				firestoreClient.UpdateLastActivity(ctx, chat.ChatID, activities[0].Id)
			}

			if chat.IsWaitingForJules && julesFinished {
				firestoreClient.UpdateIsWaitingForJules(ctx, chat.ChatID, false)
			}
		}

		// Re-fetch chats to see if they are still waiting after processing
		// to avoid infinite loops when we set IsWaitingForJules to false
		chatsAfter, _ := firestoreClient.GetAllChats(ctx)
		anyWaitingNow := false
		for _, c := range chatsAfter {
			if c.IsWaitingForJules {
				anyWaitingNow = true
				break
			}
		}

		if !anyWaitingNow {
			log.Println("No tasks waiting for Jules. Exiting long-running poller.")
			break
		}

		if minInteractiveInterval == 999999 {
			minInteractiveInterval = 5
		}

		// Sleep for the minimum interactive interval across waiting chats
		time.Sleep(time.Duration(minInteractiveInterval) * time.Second)
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
