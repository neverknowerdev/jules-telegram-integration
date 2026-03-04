package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
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
	selectedSources []string
)

func initEnv() {
	projectID = os.Getenv("GCP_PROJECT")
	if projectID == "" {
		projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}

	apiKey := os.Getenv("JULES_API_KEY")
	telegramToken := os.Getenv("TELEGRAM_TOKEN")
	sourcesStr := os.Getenv("SELECTED_SOURCES")
	if sourcesStr != "" {
		selectedSources = strings.Split(sourcesStr, ",")
	}

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

	// Handle inline keyboard button presses
	if update.CallbackQuery != nil {
		cq := update.CallbackQuery
		handleCallback(ctx, cq.Message.Chat.ID, cq.ID, cq.Data, cq.Message.MessageID)
		w.WriteHeader(http.StatusOK)
		return
	}

	if update.Message == nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	chatID := update.Message.Chat.ID
	text := update.Message.Text

	if !strings.HasPrefix(text, "/") && text != "🗓 Show Tasks" && text != "➕ New Task" {
		handleMessage(ctx, chatID, text)
	} else {
		handleCommand(ctx, chatID, text)
	}

	w.WriteHeader(http.StatusOK)
}

func handleCommand(ctx context.Context, chatID int64, text string) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return
	}
	command := parts[0]
	if text == "🗓 Show Tasks" || text == "➕ New Task" || text == "📦 Archive Chat" {
		command = text
	}

	// Ensure clients are ready
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

		keyboard := telegram.ReplyKeyboardMarkup{
			Keyboard: [][]telegram.KeyboardButton{
				{
					{Text: "🗓 Show Tasks"},
					{Text: "➕ New Task"},
				},
			},
			ResizeKeyboard: true,
			IsPersistent:   true,
		}

		msg := "Welcome! I am bound to your Jules repositories.\nUse the menu to navigate."
		telegramClient.SendMessageWithReplyKeyboard(chatID, msg, keyboard)

	case "/tasks", "/sessions", "🗓 Show Tasks":
		sessions, err := julesClient.ListSessions()
		if err != nil {
			log.Printf("Failed to list sessions: %v", err)
			telegramClient.SendMessage(chatID, "Failed to list sessions.")
			return
		}

		chatConfig, _ := firestoreClient.GetChatConfig(ctx, chatID)
		currentSession := ""
		if chatConfig != nil {
			currentSession = chatConfig.CurrentSession
		}

		groupsMap := make(map[string][]jules.Session)
		for _, s := range sessions {
			t, err := time.Parse(time.RFC3339Nano, s.UpdateTime)
			if err == nil && time.Since(t) > 48*time.Hour {
				continue
			}

			isValidSource := len(selectedSources) == 0
			for _, src := range selectedSources {
				if s.SourceContext.Source == src {
					isValidSource = true
					break
				}
			}
			if !isValidSource {
				continue
			}
			source := s.SourceContext.Source
			parts := strings.Split(source, "/")
			if len(parts) >= 4 && parts[0] == "sources" && parts[1] == "github" {
				source = parts[2] + "/" + parts[3]
			}
			groupsMap[source] = append(groupsMap[source], s)
		}

		if len(groupsMap) == 0 {
			telegramClient.SendMessage(chatID, "No active sessions found for these sources.")
			return
		}

		for k := range groupsMap {
			sort.Slice(groupsMap[k], func(i, j int) bool {
				t1, _ := time.Parse(time.RFC3339Nano, groupsMap[k][i].UpdateTime)
				t2, _ := time.Parse(time.RFC3339Nano, groupsMap[k][j].UpdateTime)
				return t1.After(t2)
			})
		}

		// Build the inline keyboard
		var msgBuilder strings.Builder
		msgBuilder.WriteString("💬 <b>Available Chats (last 48h):</b>")

		var buttons [][]telegram.InlineKeyboardButton
		num := 1

		for source, grpSessions := range groupsMap {
			msgBuilder.WriteString(fmt.Sprintf("\n\n<b>%s</b>", source))

			for _, s := range grpSessions {
				t, _ := time.Parse(time.RFC3339Nano, s.UpdateTime)
				relTime := relativeTime(t)

				isCurrent := currentSession == s.Name
				currentMark := ""
				if isCurrent {
					currentMark = " ⭐"
				}

				cleanTitle := strings.ReplaceAll(s.Title, "\n", " ")
				runes := []rune(cleanTitle)
				if len(runes) > 50 {
					cleanTitle = string(runes[:47]) + "..."
				}

				msgBuilder.WriteString(fmt.Sprintf("\n%d. %s%s <i>(%s | %s)</i>",
					num, cleanTitle, currentMark, s.State, relTime))

				sessionIDShort := strings.TrimPrefix(s.Name, "sessions/")
				buttonLabel := fmt.Sprintf("%d", num)
				if isCurrent {
					buttonLabel = "⭐ " + buttonLabel
				}
				btn := telegram.InlineKeyboardButton{
					Text:         buttonLabel,
					CallbackData: "switch:" + sessionIDShort,
				}
				// Group 5 buttons per row
				rowIdx := (num - 1) / 5
				for len(buttons) <= rowIdx {
					buttons = append(buttons, []telegram.InlineKeyboardButton{})
				}
				buttons[rowIdx] = append(buttons[rowIdx], btn)

				num++
			}
		}

		keyboard := telegram.InlineKeyboardMarkup{InlineKeyboard: buttons}
		telegramClient.SendMessageWithKeyboard(chatID, msgBuilder.String(), keyboard)

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

	case "/new_chat", "➕ New Task":
		var msgBuilder strings.Builder
		msgBuilder.WriteString("💬 <b>Select repository for the new chat:</b>\n")

		var buttons [][]telegram.InlineKeyboardButton

		sources, err := julesClient.ListSources()
		if err != nil {
			telegramClient.SendMessage(chatID, "Failed to load repos.")
			return
		}

		for _, s := range sources {
			// Limit to selected sources if specified
			if len(selectedSources) > 0 {
				found := false
				for _, sel := range selectedSources {
					if s.Name == sel {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}

			repoName := s.GithubRepo.Repo
			if repoName == "" {
				repoName = s.DisplayName
			}
			if repoName == "" {
				repoName = "Unknown Repo"
			}

			btn := telegram.InlineKeyboardButton{
				Text:         repoName,
				CallbackData: "newchat:" + repoName,
			}
			buttons = append(buttons, []telegram.InlineKeyboardButton{btn})
		}

		if len(buttons) == 0 {
			telegramClient.SendMessage(chatID, "No repositories available.")
			return
		}

		// clear state in case it was stuck
		firestoreClient.UpdateChatState(ctx, chatID, "", "")

		keyboard := telegram.InlineKeyboardMarkup{InlineKeyboard: buttons}
		telegramClient.SendMessageWithKeyboard(chatID, msgBuilder.String(), keyboard)

	case "/switch":
		telegramClient.SendMessage(chatID, "Please use /tasks to select a chat via buttons.")

	default:
		telegramClient.SendMessage(chatID, "Unknown command.")
	}
}

// handleCallback processes inline keyboard button presses
func handleCallback(ctx context.Context, chatID int64, callbackID string, data string, messageID int) {
	if firestoreClient == nil || julesClient == nil || telegramClient == nil {
		return
	}

	// Acknowledge the press so the spinner disappears
	telegramClient.AnswerCallbackQuery(callbackID, "Switching...")

	// Handle New Chat callback
	if strings.HasPrefix(data, "newchat:") {
		repoName := strings.TrimPrefix(data, "newchat:")
		telegramClient.AnswerCallbackQuery(callbackID, "")

		// Need to find full source URL
		var fullSource string
		sources, err := julesClient.ListSources()
		if err == nil {
			for _, s := range sources {
				if s.GithubRepo.Repo == repoName || s.DisplayName == repoName {
					fullSource = s.Name
					break
				}
			}
		}

		if fullSource == "" {
			telegramClient.SendMessage(chatID, "Could not identify the repository.")
			return
		}

		if err := firestoreClient.UpdateChatState(ctx, chatID, "waiting_for_message", fullSource); err != nil {
			log.Printf("Failed to update chat state: %v", err)
			telegramClient.SendMessage(chatID, "An error occurred.")
			return
		}
		if err := firestoreClient.UpdateCreationMode(ctx, chatID, "interactive"); err != nil {
			log.Printf("Failed to update creation mode: %v", err)
		}

		var modeButtons [][]telegram.InlineKeyboardButton
		modeButtons = append(modeButtons, []telegram.InlineKeyboardButton{
			{Text: "💡 Interactive (Default)", CallbackData: "mode:interactive"},
			{Text: "🚀 Start", CallbackData: "mode:start"},
		})
		modeButtons = append(modeButtons, []telegram.InlineKeyboardButton{
			{Text: "👀 Review", CallbackData: "mode:review"},
			{Text: "⏳ Scheduled", CallbackData: "mode:scheduled"},
		})

		keyboard := telegram.InlineKeyboardMarkup{InlineKeyboard: modeButtons}
		msg := fmt.Sprintf("✏️ <b>Repository:</b> %s\nMode selected: <b>interactive</b>\n\n<i>You can change it using buttons below.</i>\n\nPlease enter the initial message for this new task:", repoName)
		telegramClient.SendMessageWithKeyboard(chatID, msg, keyboard)
		return
	}

	// Handle Mode selection callback
	if strings.HasPrefix(data, "mode:") {
		mode := strings.TrimPrefix(data, "mode:")
		telegramClient.AnswerCallbackQuery(callbackID, "Mode updated")

		chatConfig, err := firestoreClient.GetChatConfig(ctx, chatID)
		if err != nil {
			return
		}

		if err := firestoreClient.UpdateCreationMode(ctx, chatID, mode); err != nil {
			log.Printf("Failed to update creation mode: %v", err)
			return
		}

		// Edit current message to reflect change
		repoPart := ""
		parts := strings.Split(chatConfig.DraftSource, "/")
		if len(parts) >= 2 {
			repoPart = parts[len(parts)-1]
		}

		var modeButtons [][]telegram.InlineKeyboardButton
		modeButtons = append(modeButtons, []telegram.InlineKeyboardButton{
			{Text: "💡 Interactive", CallbackData: "mode:interactive"},
			{Text: "🚀 Start", CallbackData: "mode:start"},
		})
		modeButtons = append(modeButtons, []telegram.InlineKeyboardButton{
			{Text: "👀 Review", CallbackData: "mode:review"},
			{Text: "⏳ Scheduled", CallbackData: "mode:scheduled"},
		})

		// Highlight selected
		for i, row := range modeButtons {
			for j, btn := range row {
				if btn.CallbackData == "mode:"+mode {
					modeButtons[i][j].Text = "✅ " + btn.Text
				}
			}
		}

		keyboard := telegram.InlineKeyboardMarkup{InlineKeyboard: modeButtons}
		newMsg := fmt.Sprintf("✏️ <b>Repository:</b> %s\nMode selected: <b>%s</b>\n\n<i>You can change it using buttons below.</i>\n\nPlease enter the initial message for this new task:", repoPart, mode)
		telegramClient.EditMessageText(chatID, messageID, newMsg, &keyboard)
		return
	}

	// Handle Approve Plan callback
	if strings.HasPrefix(data, "approve_plan:") {
		sessionIDShort := strings.TrimPrefix(data, "approve_plan:")
		sessionName := "sessions/" + sessionIDShort

		telegramClient.AnswerCallbackQuery(callbackID, "Approving...")
		if err := julesClient.ApprovePlan(sessionName); err != nil {
			log.Printf("Failed to approve plan: %v", err)
			telegramClient.SendMessage(chatID, fmt.Sprintf("❌ Failed to approve plan: %v", err))
		} else {
			telegramClient.SendMessage(chatID, "✅ Plan approved successfully!")
			// Reset completion_sent flag for this session
			firestoreClient.SetCompletionSent(ctx, chatID, false)
		}
		return
	}

	// Handle Create PR callback
	if strings.HasPrefix(data, "create_pr:") {
		sessionIDShort := strings.TrimPrefix(data, "create_pr:")
		sessionName := "sessions/" + sessionIDShort

		telegramClient.AnswerCallbackQuery(callbackID, "Sending 'Create PR' to Jules...")
		if err := julesClient.SendMessage(sessionName, "Create PR"); err != nil {
			log.Printf("Failed to send PR request to Jules: %v", err)
			telegramClient.SendMessage(chatID, fmt.Sprintf("❌ Failed to send request: %v", err))
		} else {
			telegramClient.SendMessage(chatID, "🚀 Sent 'Create PR' command to Jules. Working...")
			// Reset flags to pick up new progress
			firestoreClient.SetCompletionSent(ctx, chatID, false)
		}
		return
	}

	// Handle Create Branch callback
	if strings.HasPrefix(data, "create_branch:") {
		sessionIDShort := strings.TrimPrefix(data, "create_branch:")
		sessionName := "sessions/" + sessionIDShort

		telegramClient.AnswerCallbackQuery(callbackID, "Sending 'Create Branch' to Jules...")
		if err := julesClient.SendMessage(sessionName, "Create Branch"); err != nil {
			log.Printf("Failed to send Branch request to Jules: %v", err)
			telegramClient.SendMessage(chatID, fmt.Sprintf("❌ Failed to send request: %v", err))
		} else {
			telegramClient.SendMessage(chatID, "🚀 Sent 'Create Branch' command to Jules. Working...")
			// Reset flags to pick up new progress
			firestoreClient.SetCompletionSent(ctx, chatID, false)
		}
		return
	}

	if !strings.HasPrefix(data, "switch:") {
		return
	}
	sessionIDShort := strings.TrimPrefix(data, "switch:")
	sessionID := "sessions/" + sessionIDShort

	if err := firestoreClient.UpdateCurrentSession(ctx, chatID, sessionID); err != nil {
		log.Printf("Failed to switch session: %v", err)
		telegramClient.SendMessage(chatID, "Failed to switch session.")
		return
	}

	// Set the keyboard to Navigation modes
	keyboard := telegram.ReplyKeyboardMarkup{
		Keyboard: [][]telegram.KeyboardButton{
			{
				{Text: "📦 Archive Chat"},
				{Text: "🏠 Main Menu"},
			},
		},
		ResizeKeyboard: true,
		IsPersistent:   true,
	}
	telegramClient.SendMessageWithReplyKeyboard(chatID, fmt.Sprintf("✅ Switched to session <code>%s</code>", sessionIDShort), keyboard)

	// Fetch latest activities to show context
	activities, err := julesClient.ListActivities(sessionID)
	if err == nil && len(activities) > 0 {
		var latestJules *jules.Activity
		var latestUser *jules.Activity

		absoluteLatest := activities[0]

		for i := range activities {
			act := &activities[i]
			if (act.Originator == "agent" || act.Originator == "system") && latestJules == nil {
				latestJules = act
			} else if act.Originator == "user" && latestUser == nil {
				latestUser = act
			}
			if latestJules != nil && latestUser != nil {
				break
			}
		}

		if absoluteLatest.Originator == "user" && latestUser != nil && latestJules != nil {
			telegramClient.SendMessage(chatID, formatActivity(*latestJules))
			telegramClient.SendMessage(chatID, formatActivity(*latestUser))
		} else if latestJules != nil {
			telegramClient.SendMessage(chatID, formatActivity(*latestJules))
		} else if latestUser != nil {
			telegramClient.SendMessage(chatID, formatActivity(*latestUser))
		}
	}
}

func relativeTime(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	diff := time.Since(t)
	if diff < time.Minute {
		return "just now"
	} else if diff < time.Hour {
		return fmt.Sprintf("%d mins ago", int(diff.Minutes()))
	} else if diff < 24*time.Hour {
		return fmt.Sprintf("%d hrs ago", int(diff.Hours()))
	}
	return fmt.Sprintf("%d days ago", int(diff.Hours()/24))
}

func formatActivity(act jules.Activity) string {
	title := "New Activity"
	desc := ""
	if act.ProgressUpdated.Title != "" {
		title = act.ProgressUpdated.Title
		desc = act.ProgressUpdated.Description
	} else if len(act.PlanGenerated.Plan.Steps) > 0 {
		title = "Plan Generated"
		desc = formatTelegramHTML(act.PlanGenerated.Plan.Title)
		if desc != "" {
			desc += "\n\n"
		}
		for i, step := range act.PlanGenerated.Plan.Steps {
			stepTitle := formatTelegramHTML(step.Title)
			desc += fmt.Sprintf("<b>%d. %s</b>\n", i+1, stepTitle)
			if step.Description != "" {
				cleanDesc := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(step.Description), "-"))
				desc += fmt.Sprintf("<i>%s</i>\n", formatTelegramHTML(cleanDesc))
			}
			desc += "\n"
		}
		desc = strings.TrimSpace(desc)
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

func handleMessage(ctx context.Context, chatID int64, text string) {
	if firestoreClient == nil || julesClient == nil || telegramClient == nil {
		log.Println("Clients not initialized")
		return
	}

	chatConfig, err := firestoreClient.GetChatConfig(ctx, chatID)

	// Intercept Archive/Menu commands from keyboard
	if text == "📦 Archive Chat" || text == "🏠 Main Menu" {
		if text == "🏠 Main Menu" {
			keyboard := telegram.ReplyKeyboardMarkup{
				Keyboard: [][]telegram.KeyboardButton{
					{
						{Text: "🗓 Show Tasks"},
						{Text: "➕ New Task"},
					},
				},
				ResizeKeyboard: true,
				IsPersistent:   true,
			}
			telegramClient.SendMessageWithReplyKeyboard(chatID, "Main menu:", keyboard)
			return
		}

		if text == "📦 Archive Chat" {
			if chatConfig == nil || chatConfig.CurrentSession == "" {
				telegramClient.SendMessage(chatID, "No active session to archive.")
				return
			}

			telegramClient.SendMessage(chatID, "⏳ Archiving session...")
			if err := julesClient.ArchiveSession(chatConfig.CurrentSession); err != nil {
				log.Printf("Failed to archive session: %v", err)
				telegramClient.SendMessage(chatID, fmt.Sprintf("Failed to archive session: %v", err))
				return
			}

			// Clear current session in firestore
			firestoreClient.UpdateCurrentSession(ctx, chatID, "")

			keyboard := telegram.ReplyKeyboardMarkup{
				Keyboard: [][]telegram.KeyboardButton{
					{
						{Text: "🗓 Show Tasks"},
						{Text: "➕ New Task"},
					},
				},
				ResizeKeyboard: true,
				IsPersistent:   true,
			}
			telegramClient.SendMessageWithReplyKeyboard(chatID, "✅ Session archived successfully.", keyboard)
			return
		}
		return
	}

	// Intercept Prev/Next commands from keyboard (Deprecating but keeping code block structure for a moment if needed, actually user said remove it)
	if text == "⬅️ Prev Task" || text == "Next Task ➡️" {
		telegramClient.SendMessage(chatID, "Previous/Next navigation is disabled. Use 'Show Tasks' to switch.")
		return
	}

	// Handle creation flow
	if err == nil && chatConfig.State == "waiting_for_message" {
		telegramClient.SendMessage(chatID, "⏳ Creating session on Jules...")

		session, err := julesClient.CreateSession(text, chatConfig.DraftSource, chatConfig.CreationMode)
		if err != nil {
			telegramClient.SendMessage(chatID, fmt.Sprintf("Failed to create session: %v", err))
			firestoreClient.UpdateChatState(ctx, chatID, "", "")
			return
		}

		// Switch current session to this new session
		firestoreClient.UpdateCurrentSession(ctx, chatID, session.Name)
		firestoreClient.UpdateChatState(ctx, chatID, "", "")

		keyboard := telegram.ReplyKeyboardMarkup{
			Keyboard: [][]telegram.KeyboardButton{
				{
					{Text: "📦 Archive Chat"},
					{Text: "🏠 Main Menu"},
				},
			},
			ResizeKeyboard: true,
			IsPersistent:   true,
		}

		telegramClient.SendMessageWithReplyKeyboard(chatID, fmt.Sprintf("✅ <b>New Chat Created!</b>\nSwitched session to: <code>%s</code>\n", strings.TrimPrefix(session.Name, "sessions/")), keyboard)
		return
	}

	if err != nil || chatConfig.CurrentSession == "" {
		telegramClient.SendMessage(chatID, "No active session. Use /tasks to select one or /new_chat to create one.")
		return
	}

	if err := julesClient.SendMessage(chatConfig.CurrentSession, text); err != nil {
		log.Printf("Failed to send message to Jules: %v", err)
		telegramClient.SendMessage(chatID, "Failed to send message to Jules.")
		return
	}
}
