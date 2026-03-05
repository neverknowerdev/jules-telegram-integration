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
	"github.com/neverknowerdev/jules-telegram-bot/internal/telegraph"
)

var (
	julesClient     jules.ClientInterface
	firestoreClient firestore.ClientInterface
	telegramClient  telegram.ClientInterface
	telegraphClient *telegraph.Client
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

	telegraphToken := os.Getenv("TELEGRAPH_ACCESS_TOKEN")
	if telegraphToken != "" {
		telegraphClient = telegraph.NewClient(telegraphToken)
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
		realFirestoreClient, err := firestore.NewClient(ctx, projectID)
		if err != nil {
			log.Printf("Failed to create Firestore client: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		firestoreClient = realFirestoreClient
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
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[PANIC] in handleCallback: %v", r)
					telegramClient.SendMessage(cq.Message.Chat.ID, cq.Message.MessageThreadID, "A critical error occurred.")
				}
			}()
			handleCallback(ctx, cq.Message.Chat.ID, cq.ID, cq.Data, cq.Message.MessageID)
		}()
		w.WriteHeader(http.StatusOK)
		return
	}

	if update.Message == nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	chatID := update.Message.Chat.ID
	threadID := update.Message.MessageThreadID
	text := update.Message.Text

	if update.Message.ForumTopicCreated != nil {
		// Ignore topics created by bots (e.g., our bot syncing history)
		if update.Message.From != nil && update.Message.From.IsBot {
			w.WriteHeader(http.StatusOK)
			return
		}
		handleTopicCreated(ctx, chatID, threadID, update.Message.ForumTopicCreated)
		w.WriteHeader(http.StatusOK)
		return
	}

	if update.Message.ForumTopicClosed != nil {
		handleTopicClosed(ctx, chatID, threadID)
		w.WriteHeader(http.StatusOK)
		return
	}

	if !strings.HasPrefix(text, "/") && text != "🗓 Show Tasks" && text != "➕ New Task" {
		handleMessage(ctx, chatID, threadID, text)
	} else {
		handleCommand(ctx, chatID, threadID, text)
	}

	w.WriteHeader(http.StatusOK)
}

func handleTopicClosed(ctx context.Context, chatID int64, threadID int) {
	log.Printf("[WEBHOOK] Topic closed/deleted: thread_id %d", threadID)

	chatConfig, err := firestoreClient.GetChatConfig(ctx, chatID, threadID)
	if err == nil && chatConfig != nil {
		if telegraphClient != nil && telegraphClient.AccessToken != "" {
			for _, path := range chatConfig.TelegraphPages {
				// clear the content of the telegraph page
				_, err := telegraphClient.EditPage(path, "Deleted Task Logs", []telegraph.Node{
					{
						Tag:      "p",
						Children: []telegraph.NodeChild{"Logs have been deleted as the task was archived/deleted."},
					},
				})
				if err != nil {
					log.Printf("[WEBHOOK] Failed to delete telegraph page %s: %v", path, err)
				} else {
					log.Printf("[WEBHOOK] Cleared telegraph page %s", path)
				}
			}
		}

		err = firestoreClient.DeleteChatConfig(ctx, chatID, threadID)
		if err != nil {
			log.Printf("[WEBHOOK] Error deleting chat config: %v", err)
		} else {
			log.Printf("[WEBHOOK] Deleted chat config for thread %d", threadID)
		}
	}
}

func handleTopicCreated(ctx context.Context, chatID int64, threadID int, topic *telegram.ForumTopicCreated) {
	if firestoreClient == nil || julesClient == nil || telegramClient == nil {
		return
	}

	// Do not overwrite existing configs (e.g. from cloned tasks)
	existingConfig, _ := firestoreClient.GetChatConfig(ctx, chatID, threadID)
	if existingConfig != nil && existingConfig.State != "" {
		return
	}

	config := firestore.ChatConfig{
		ChatID:   chatID,
		ThreadID: threadID,
		State:    "waiting_for_repo",
	}
	if err := firestoreClient.SaveChatConfig(ctx, config); err != nil {
		log.Printf("Failed to save chat config for new topic: %v", err)
		return
	}

	sources, err := julesClient.ListSourcesSummary()
	if err != nil {
		telegramClient.SendMessage(chatID, threadID, "Failed to load repos.")
		return
	}

	var msgBuilder strings.Builder
	safeTopicName := escapeHTML(topic.Name)
	msgBuilder.WriteString(fmt.Sprintf("💬 <b>Topic '%s' Created!</b>\nSelect a repository to bind to this task:\n\n", safeTopicName))

	var buttons [][]telegram.InlineKeyboardButton
	var currentRow []telegram.InlineKeyboardButton

	idx := 1
	for _, s := range sources {
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

		msgBuilder.WriteString(fmt.Sprintf("%d. %s\n", idx, escapeHTML(repoName)))

		btn := telegram.InlineKeyboardButton{
			Text:         fmt.Sprintf("%d", idx),
			CallbackData: fmt.Sprintf("topicrepo:%d:%s", threadID, repoName),
		}
		currentRow = append(currentRow, btn)

		if len(currentRow) == 5 {
			buttons = append(buttons, currentRow)
			currentRow = nil
		}
		idx++
	}

	if len(currentRow) > 0 {
		buttons = append(buttons, currentRow)
	}

	if len(buttons) == 0 {
		telegramClient.SendMessage(chatID, threadID, "No repositories available.")
		return
	}

	keyboard := telegram.InlineKeyboardMarkup{InlineKeyboard: buttons}
	telegramClient.SendMessageWithKeyboard(chatID, threadID, msgBuilder.String(), keyboard)
}

func handleCommand(ctx context.Context, chatID int64, threadID int, text string) {
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
			telegramClient.SendMessage(chatID, threadID, "Failed to register chat.")
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
		telegramClient.SendMessageWithReplyKeyboard(chatID, threadID, msg, keyboard)

	case "/tasks", "/sessions", "🗓 Show Tasks":
		sessions, err := julesClient.ListSessions()
		if err != nil {
			log.Printf("Failed to list sessions: %v", err)
			telegramClient.SendMessage(chatID, threadID, "Failed to list sessions.")
			return
		}

		allChats, err := firestoreClient.GetChatsByChatID(ctx, chatID)
		if err != nil {
			log.Printf("Failed to get chats by chatID: %v", err)
			allChats = []firestore.ChatConfig{}
		}

		sessionToThread := make(map[string]int)
		for _, c := range allChats {
			if c.CurrentSession != "" && c.ThreadID > 0 {
				sessionToThread[c.CurrentSession] = c.ThreadID
			}
		}

		chatConfig, _ := firestoreClient.GetChatConfig(ctx, chatID, threadID)
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
			telegramClient.SendMessage(chatID, threadID, "No active sessions found for these sources.")
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

				var btn telegram.InlineKeyboardButton
				if tid, ok := sessionToThread[s.Name]; ok {
					// In a supergroup, chat ID is usually negative and starts with -100
					// URL is https://t.me/c/<id_without_-100>/<thread_id>
					var chatURL string
					strChatID := fmt.Sprintf("%d", chatID)
					if strings.HasPrefix(strChatID, "-100") {
						chatURL = fmt.Sprintf("https://t.me/c/%s/%d", strings.TrimPrefix(strChatID, "-100"), tid)
					} else {
						// Fallback if not a standard supergroup ID
						chatURL = fmt.Sprintf("https://t.me/c/%s/%d", strings.TrimPrefix(strChatID, "-"), tid)
					}
					btn = telegram.InlineKeyboardButton{
						Text: buttonLabel,
						URL:  chatURL,
					}
				} else {
					btn = telegram.InlineKeyboardButton{
						Text:         buttonLabel,
						CallbackData: "createtopic:" + sessionIDShort,
					}
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
		telegramClient.SendMessageWithKeyboard(chatID, threadID, msgBuilder.String(), keyboard)

	case "/status":
		chatConfig, err := firestoreClient.GetChatConfig(ctx, chatID, threadID)
		if err != nil {
			telegramClient.SendMessage(chatID, threadID, "Could not retrieve chat status.")
			return
		}
		if chatConfig.CurrentSession == "" {
			telegramClient.SendMessage(chatID, threadID, "No session currently selected.")
			return
		}
		telegramClient.SendMessage(chatID, threadID, fmt.Sprintf("Current Session: %s", chatConfig.CurrentSession))

	case "/new_chat", "➕ New Task":
		var msgBuilder strings.Builder
		msgBuilder.WriteString("💬 <b>Select repository for the new chat:</b>\n")

		var buttons [][]telegram.InlineKeyboardButton

		sources, err := julesClient.ListSourcesSummary()
		if err != nil {
			telegramClient.SendMessage(chatID, threadID, "Failed to load repos.")
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
			telegramClient.SendMessage(chatID, threadID, "No repositories available.")
			return
		}

		// clear state in case it was stuck
		firestoreClient.UpdateChatState(ctx, chatID, threadID, "", "")

		keyboard := telegram.InlineKeyboardMarkup{InlineKeyboard: buttons}
		telegramClient.SendMessageWithKeyboard(chatID, threadID, msgBuilder.String(), keyboard)

	case "/switch":
		telegramClient.SendMessage(chatID, threadID, "Please use /tasks to select a chat via buttons.")

	default:
		telegramClient.SendMessage(chatID, threadID, "Unknown command.")
	}
}

// handleCallback processes inline keyboard button presses
func handleCallback(ctx context.Context, chatID int64, callbackID string, data string, messageID int) {
	if firestoreClient == nil || julesClient == nil || telegramClient == nil {
		return
	}

	// Acknowledge the press so the spinner disappears
	telegramClient.AnswerCallbackQuery(callbackID, "Switching...")

	// Handle topic repo selection callback
	if strings.HasPrefix(data, "topicrepo:") {
		parts := strings.SplitN(data, ":", 3)
		if len(parts) != 3 {
			return
		}
		threadIDStr := parts[1]
		repoName := parts[2]

		var threadID int
		fmt.Sscanf(threadIDStr, "%d", &threadID)

		// Need to find full source URL
		var fullSource string
		sources, err := julesClient.ListSourcesSummary()
		if err == nil {
			for _, s := range sources {
				if s.GithubRepo.Repo == repoName || s.DisplayName == repoName {
					fullSource = s.Name
					break
				}
			}
		}

		if fullSource == "" {
			telegramClient.SendMessage(chatID, threadID, "Could not identify the repository.")
			return
		}

		if err := firestoreClient.UpdateChatState(ctx, chatID, threadID, "waiting_for_branch", fullSource); err != nil {
			log.Printf("[WEBHOOK] Failed to update chat state: %v", err)
			telegramClient.SendMessage(chatID, threadID, "An error occurred updating the task state.")
			return
		}

		source, err := julesClient.GetSource(fullSource)
		if err != nil {
			log.Printf("[WEBHOOK] Failed to get source details: %v", err)
			telegramClient.SendMessage(chatID, threadID, "Failed to load repository details.")
			return
		}

		var branchButtons [][]telegram.InlineKeyboardButton
		var currentRow []telegram.InlineKeyboardButton

		// Fetch branches for this source
		var branches []string
		for _, b := range source.GithubRepo.Branches {
			branches = append(branches, b.DisplayName)
		}

		// If no branches or just main, we can fallback, but let's provide default
		if len(branches) == 0 {
			branches = []string{"main"}
		}

		idx := 1
		safeRepoName := escapeHTML(repoName)
		msgBuilder := fmt.Sprintf("✏️ <b>Repository:</b> %s\nSelect base branch for this task:\n\n", safeRepoName)
		for i, branch := range branches {
			safeBranch := escapeHTML(branch)
			msgBuilder += fmt.Sprintf("%d. %s\n", idx, safeBranch)
			btn := telegram.InlineKeyboardButton{
				Text:         fmt.Sprintf("%d", idx),
				CallbackData: fmt.Sprintf("topicbranch:%d:%d", threadID, i),
			}
			currentRow = append(currentRow, btn)

			if len(currentRow) == 5 {
				branchButtons = append(branchButtons, currentRow)
				currentRow = nil
			}
			idx++
		}
		if len(currentRow) > 0 {
			branchButtons = append(branchButtons, currentRow)
		}

		keyboard := telegram.InlineKeyboardMarkup{InlineKeyboard: branchButtons}
		if err := telegramClient.EditMessageText(chatID, messageID, msgBuilder, &keyboard); err != nil {
			log.Printf("[WEBHOOK] Failed to edit message: %v", err)
			// fallback to sending new message if edit fails
			if err2 := telegramClient.SendMessageWithKeyboard(chatID, threadID, msgBuilder, keyboard); err2 != nil {
				log.Printf("[WEBHOOK] Fallback SendMessageWithKeyboard also failed: %v", err2)
			} else {
				log.Printf("[WEBHOOK] Fallback SendMessageWithKeyboard succeeded")
			}
		} else {
			log.Printf("[WEBHOOK] Message edited successfully")
		}
		return
	}

	// Handle topic branch selection callback
	if strings.HasPrefix(data, "topicbranch:") {
		parts := strings.SplitN(data, ":", 3)
		if len(parts) != 3 {
			return
		}
		threadIDStr := parts[1]
		branchIdxOrName := parts[2]

		var threadID int
		fmt.Sscanf(threadIDStr, "%d", &threadID)

		telegramClient.AnswerCallbackQuery(callbackID, "")

		chatConfig, err := firestoreClient.GetChatConfig(ctx, chatID, threadID)
		if err != nil || chatConfig == nil {
			telegramClient.SendMessage(chatID, threadID, "Could not find chat config.")
			return
		}

		// Resolve branch name if it's an index
		branchName := branchIdxOrName
		var idx int
		if n, err := fmt.Sscanf(branchIdxOrName, "%d", &idx); err == nil && n == 1 {
			log.Printf("[WEBHOOK] Resolving branch index %d for source %s", idx, chatConfig.DraftSource)
			source, err := julesClient.GetSource(chatConfig.DraftSource)
			if err == nil {
				var branches []string
				for _, b := range source.GithubRepo.Branches {
					branches = append(branches, b.DisplayName)
				}
				if len(branches) == 0 {
					branches = []string{"main"}
				}
				if idx >= 0 && idx < len(branches) {
					branchName = branches[idx]
					log.Printf("[WEBHOOK] Resolved index %d to branch: %s", idx, branchName)
				} else {
					log.Printf("[WEBHOOK] Index %d out of range for %d branches", idx, len(branches))
				}
			} else {
				log.Printf("[WEBHOOK] Failed to resolve branch index: %v", err)
			}
		}

		if err := firestoreClient.UpdateDraftBranch(ctx, chatID, threadID, branchName); err != nil {
			log.Printf("Failed to update draft branch: %v", err)
		}

		// If we are currently selecting a branch while waiting for title (i.e. cloned task branch change)
		if chatConfig.State == "waiting_for_title" {
			telegramClient.AnswerCallbackQuery(callbackID, "Branch updated")
			// Update the prompt message inline keyboard to reflect the new selected branch
			// We can re-fetch branches and recreate the inline keyboard
			var branches []string
			source, _ := julesClient.GetSource(chatConfig.DraftSource)
			if source != nil {
				for _, b := range source.GithubRepo.Branches {
					branches = append(branches, b.DisplayName)
				}
			}
			if len(branches) == 0 {
				branches = []string{"main"}
			}

			var branchButtons [][]telegram.InlineKeyboardButton
			var currentRow []telegram.InlineKeyboardButton
			idx := 1

			repoPart := chatConfig.DraftSource
			sourceParts := strings.Split(chatConfig.DraftSource, "/")
			if len(sourceParts) >= 2 {
				repoPart = sourceParts[len(sourceParts)-1]
			}

			msgBuilder := fmt.Sprintf("✏️ <b>Repository:</b> %s\n🌿 <b>Base branch:</b> %s\nTask cloned! Select another base branch below, or reply with a new title for this task:\n\n", escapeHTML(repoPart), escapeHTML(branchName))

			for i, branch := range branches {
				btnText := fmt.Sprintf("%d", idx)
				if branch == branchName {
					btnText = "✅ " + btnText
				}
				btn := telegram.InlineKeyboardButton{
					Text:         btnText,
					CallbackData: fmt.Sprintf("topicbranch:%d:%d", threadID, i),
				}
				currentRow = append(currentRow, btn)
				if len(currentRow) == 5 {
					branchButtons = append(branchButtons, currentRow)
					currentRow = nil
				}
				idx++
			}
			if len(currentRow) > 0 {
				branchButtons = append(branchButtons, currentRow)
			}
			keyboard := telegram.InlineKeyboardMarkup{InlineKeyboard: branchButtons}
			telegramClient.EditMessageText(chatID, messageID, msgBuilder, &keyboard)
			return
		}

		if err := firestoreClient.UpdateChatState(ctx, chatID, threadID, "waiting_for_message", chatConfig.DraftSource); err != nil {
			log.Printf("Failed to update chat state: %v", err)
			telegramClient.SendMessage(chatID, threadID, "An error occurred.")
			return
		}
		if err := firestoreClient.UpdateCreationMode(ctx, chatID, threadID, "interactive"); err != nil {
			log.Printf("Failed to update creation mode: %v", err)
		}

		repoPart := ""
		sourceParts := strings.Split(chatConfig.DraftSource, "/")
		if len(sourceParts) >= 2 {
			repoPart = sourceParts[len(sourceParts)-1]
		}

		var modeButtons [][]telegram.InlineKeyboardButton
		modeButtons = append(modeButtons, []telegram.InlineKeyboardButton{
			{Text: "💡 Interactive (Default)", CallbackData: fmt.Sprintf("mode:%d:interactive", threadID)},
			{Text: "🚀 Start", CallbackData: fmt.Sprintf("mode:%d:start", threadID)},
		})
		modeButtons = append(modeButtons, []telegram.InlineKeyboardButton{
			{Text: "👀 Review", CallbackData: fmt.Sprintf("mode:%d:review", threadID)},
			{Text: "⏳ Scheduled", CallbackData: fmt.Sprintf("mode:%d:scheduled", threadID)},
		})

		keyboard := telegram.InlineKeyboardMarkup{InlineKeyboard: modeButtons}
		msg := fmt.Sprintf("✏️ <b>Repository:</b> %s\n🌿 <b>Base branch:</b> %s\nMode selected: <b>interactive</b>\n\n<i>You can change it using buttons below.</i>\n\nPlease enter the initial message for this new task:", repoPart, branchName)
		telegramClient.EditMessageText(chatID, messageID, msg, &keyboard)
		return
	}

	// Handle New Chat callback (Old flow)
	if strings.HasPrefix(data, "newchat:") {
		repoName := strings.TrimPrefix(data, "newchat:")
		telegramClient.AnswerCallbackQuery(callbackID, "")

		// Need to find full source URL
		var fullSource string
		sources, err := julesClient.ListSourcesSummary()
		if err == nil {
			for _, s := range sources {
				if s.GithubRepo.Repo == repoName || s.DisplayName == repoName {
					fullSource = s.Name
					break
				}
			}
		}

		if fullSource == "" {
			telegramClient.SendMessage(chatID, 0, "Could not identify the repository.")
			return
		}

		source, err := julesClient.GetSource(fullSource)
		if err != nil {
			telegramClient.SendMessage(chatID, 0, "Failed to load repository details.")
			return
		}

		if err := firestoreClient.UpdateChatState(ctx, chatID, 0, "waiting_for_branch", fullSource); err != nil {
			log.Printf("Failed to update chat state: %v", err)
			telegramClient.SendMessage(chatID, 0, "An error occurred.")
			return
		}

		var branchButtons [][]telegram.InlineKeyboardButton
		var currentRow []telegram.InlineKeyboardButton

		var branches []string
		for _, b := range source.GithubRepo.Branches {
			branches = append(branches, b.DisplayName)
		}

		if len(branches) == 0 {
			branches = []string{"main"}
		}

		idx := 1
		safeRepoName := escapeHTML(repoName)
		msgBuilder := fmt.Sprintf("✏️ <b>Repository:</b> %s\nSelect base branch for this task:\n\n", safeRepoName)
		for i, branch := range branches {
			safeBranch := escapeHTML(branch)
			msgBuilder += fmt.Sprintf("%d. %s\n", idx, safeBranch)
			btn := telegram.InlineKeyboardButton{
				Text:         fmt.Sprintf("%d", idx),
				CallbackData: fmt.Sprintf("topicbranch:0:%d", i),
			}
			currentRow = append(currentRow, btn)

			if len(currentRow) == 5 {
				branchButtons = append(branchButtons, currentRow)
				currentRow = nil
			}
			idx++
		}
		if len(currentRow) > 0 {
			branchButtons = append(branchButtons, currentRow)
		}

		keyboard := telegram.InlineKeyboardMarkup{InlineKeyboard: branchButtons}
		telegramClient.SendMessageWithKeyboard(chatID, 0, msgBuilder, keyboard)
		return
	}

	// Handle Mode selection callback
	if strings.HasPrefix(data, "mode:") {
		parts := strings.SplitN(data, ":", 3)
		if len(parts) != 3 {
			return
		}
		threadIDStr := parts[1]
		mode := parts[2]

		var threadID int
		fmt.Sscanf(threadIDStr, "%d", &threadID)

		telegramClient.AnswerCallbackQuery(callbackID, "Mode updated")

		chatConfig, err := firestoreClient.GetChatConfig(ctx, chatID, threadID)
		if err != nil {
			return
		}

		if err := firestoreClient.UpdateCreationMode(ctx, chatID, threadID, mode); err != nil {
			log.Printf("Failed to update creation mode: %v", err)
			return
		}

		// Edit current message to reflect change
		repoPart := ""
		sourceParts := strings.Split(chatConfig.DraftSource, "/")
		if len(sourceParts) >= 2 {
			repoPart = sourceParts[len(sourceParts)-1]
		}

		var modeButtons [][]telegram.InlineKeyboardButton
		modeButtons = append(modeButtons, []telegram.InlineKeyboardButton{
			{Text: "💡 Interactive", CallbackData: fmt.Sprintf("mode:%d:interactive", threadID)},
			{Text: "🚀 Start", CallbackData: fmt.Sprintf("mode:%d:start", threadID)},
		})
		modeButtons = append(modeButtons, []telegram.InlineKeyboardButton{
			{Text: "👀 Review", CallbackData: fmt.Sprintf("mode:%d:review", threadID)},
			{Text: "⏳ Scheduled", CallbackData: fmt.Sprintf("mode:%d:scheduled", threadID)},
		})

		// Highlight selected
		for i, row := range modeButtons {
			for j, btn := range row {
				if btn.CallbackData == fmt.Sprintf("mode:%d:%s", threadID, mode) {
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

		// Determine the thread ID associated with this session
		var targetThreadID int
		if chats, err := firestoreClient.GetChatsByChatID(ctx, chatID); err == nil {
			for _, chat := range chats {
				if chat.CurrentSession == sessionName {
					targetThreadID = chat.ThreadID
					break
				}
			}
		}

		telegramClient.AnswerCallbackQuery(callbackID, "Approving...")
		if err := julesClient.ApprovePlan(sessionName); err != nil {
			log.Printf("Failed to approve plan: %v", err)
			telegramClient.SendMessage(chatID, targetThreadID, fmt.Sprintf("❌ Failed to approve plan: %v", err))
		} else {
			telegramClient.SendMessage(chatID, targetThreadID, "✅ Plan approved successfully!")
			// Reset progress message ID so a new log block starts after user input
			firestoreClient.UpdateProgressMessageID(ctx, chatID, targetThreadID, 0)
		}
		return
	}

	// Handle Clone callback
	if strings.HasPrefix(data, "clone:") {
		sessionIDShort := strings.TrimPrefix(data, "clone:")
		sessionID := "sessions/" + sessionIDShort

		telegramClient.AnswerCallbackQuery(callbackID, "Cloning task...")

		session, err := julesClient.GetSession(sessionID)
		if err != nil {
			telegramClient.SendMessage(chatID, 0, "❌ Failed to retrieve session for cloning.")
			return
		}

		cleanTitle := strings.ReplaceAll(session.Title, "\n", " ")
		runes := []rune(cleanTitle)
		if len(runes) > 40 {
			cleanTitle = string(runes[:37]) + "..."
		}
		newTitle := cleanTitle + " Cloned"

		newThreadID, err := telegramClient.CreateForumTopic(chatID, newTitle)
		if err != nil {
			log.Printf("Failed to create cloned topic: %v", err)
			telegramClient.SendMessage(chatID, 0, "❌ Failed to create cloned topic.")
			return
		}

		var fullSource string
		fullSource = session.SourceContext.Source

		// Since we are in handleCallback, we don't have the `update` object.
		// But we DO have `messageID` and `chatID`. If we need the `threadID` to fetch the parent config, we can fetch it via the chat context.
		// Let's just find the thread ID for the original session.
		var parentThreadID int
		if chats, err := firestoreClient.GetChatsByChatID(ctx, chatID); err == nil {
			for _, c := range chats {
				if c.CurrentSession == sessionID {
					parentThreadID = c.ThreadID
					break
				}
			}
		}

		draftBranch := "main"
		if parentThreadID > 0 {
			if parentConfig, errConfig := firestoreClient.GetChatConfig(ctx, chatID, parentThreadID); errConfig == nil && parentConfig != nil {
				if parentConfig.DraftBranch != "" {
					draftBranch = parentConfig.DraftBranch
				}
			}
		}

		fullSource = session.SourceContext.Source

		// Set new topic state to waiting for title, automatically setting branch
		// We use SaveChatConfig because this is a brand new topic/document
		newConfig := firestore.ChatConfig{
			ChatID:       chatID,
			ThreadID:     newThreadID,
			State:        "waiting_for_title",
			DraftSource:  fullSource,
			DraftBranch:  draftBranch,
			CreationMode: "interactive",
		}
		if err := firestoreClient.SaveChatConfig(ctx, newConfig); err != nil {
			log.Printf("Failed to save cloned chat config: %v", err)
			return
		}

		var repoPart string
		sourceParts := strings.Split(fullSource, "/")
		if len(sourceParts) >= 2 {
			repoPart = sourceParts[len(sourceParts)-1]
		} else {
			repoPart = fullSource
		}

		var branchButtons [][]telegram.InlineKeyboardButton
		var currentRow []telegram.InlineKeyboardButton

		var branches []string
		sources, _ := julesClient.ListSources()
		for _, s := range sources {
			if s.Name == fullSource {
				for _, b := range s.GithubRepo.Branches {
					branches = append(branches, b.DisplayName)
				}
				break
			}
		}

		if len(branches) == 0 {
			branches = []string{"main"}
		}

		idx := 1
		msgBuilder := fmt.Sprintf("✏️ <b>Repository:</b> %s\n🌿 <b>Base branch:</b> %s\nTask cloned! Select another base branch below, or reply with a new title for this task:\n\n", repoPart, draftBranch)
		for _, branch := range branches {
			btnText := fmt.Sprintf("%d", idx)
			if branch == draftBranch {
				btnText = "✅ " + btnText
			}
			msgBuilder += fmt.Sprintf("%d. %s\n", idx, branch)
			btn := telegram.InlineKeyboardButton{
				Text:         btnText,
				CallbackData: fmt.Sprintf("topicbranch:%d:%s", newThreadID, branch),
			}
			currentRow = append(currentRow, btn)

			if len(currentRow) == 5 {
				branchButtons = append(branchButtons, currentRow)
				currentRow = nil
			}
			idx++
		}
		if len(currentRow) > 0 {
			branchButtons = append(branchButtons, currentRow)
		}

		keyboard := telegram.InlineKeyboardMarkup{InlineKeyboard: branchButtons}

		// We send this message to the NEW thread
		telegramClient.SendMessageWithKeyboard(chatID, newThreadID, msgBuilder, keyboard)

		// Also notify in the current thread
		var currentThreadIDFound int
		if chats, err := firestoreClient.GetChatsByChatID(ctx, chatID); err == nil {
			for _, c := range chats {
				if c.CurrentSession == sessionID {
					currentThreadIDFound = c.ThreadID
					break
				}
			}
		}

		telegramClient.SendMessage(chatID, currentThreadIDFound, "✅ Task cloned! Please switch to the new topic to continue.")
		return
	}

	// Handle Create PR callback
	if strings.HasPrefix(data, "create_pr:") {
		sessionIDShort := strings.TrimPrefix(data, "create_pr:")
		sessionName := "sessions/" + sessionIDShort

		telegramClient.AnswerCallbackQuery(callbackID, "Sending 'Create PR' to Jules...")
		if err := julesClient.SendMessage(sessionName, "Create PR"); err != nil {
			log.Printf("Failed to send PR request to Jules: %v", err)
			telegramClient.SendMessage(chatID, 0, fmt.Sprintf("❌ Failed to send request: %v", err))
		} else {
			telegramClient.SendMessage(chatID, 0, "🚀 Sent 'Create PR' command to Jules. Working...")
		}
		return
	}

	// Handle Create Topic callback
	if strings.HasPrefix(data, "createtopic:") {
		sessionIDShort := strings.TrimPrefix(data, "createtopic:")
		sessionID := "sessions/" + sessionIDShort

		telegramClient.AnswerCallbackQuery(callbackID, "Creating topic and syncing history...")

		session, err := julesClient.GetSession(sessionID)
		if err != nil {
			log.Printf("Failed to get session: %v", err)
			telegramClient.SendMessage(chatID, 0, "❌ Failed to retrieve session.")
			return
		}

		cleanTitle := strings.ReplaceAll(session.Title, "\n", " ")
		runes := []rune(cleanTitle)
		if len(runes) > 50 {
			cleanTitle = string(runes[:47]) + "..."
		}
		if cleanTitle == "" {
			cleanTitle = "Imported Task"
		}

		newThreadID, err := telegramClient.CreateForumTopic(chatID, cleanTitle)
		if err != nil {
			log.Printf("Failed to create topic: %v", err)
			telegramClient.SendMessage(chatID, 0, "❌ Failed to create topic. Make sure the bot is an admin with 'Manage Topics' permission.")
			return
		}

		// Bind this topic to the session
		config := firestore.ChatConfig{
			ChatID:         chatID,
			ThreadID:       newThreadID,
			State:          session.State,
			CurrentSession: sessionID,
			DraftSource:    session.SourceContext.Source,
		}
		if err := firestoreClient.SaveChatConfig(ctx, config); err != nil {
			log.Printf("Failed to save chat config for created topic: %v", err)
		}

		// Fetch complete history
		activities, err := julesClient.ListAllActivities(sessionID)
		if err != nil {
			log.Printf("Failed to fetch activities: %v", err)
			telegramClient.SendMessage(chatID, newThreadID, "❌ Failed to fetch session history.")
		} else {
			// The Jules API returns activities in chronological order (oldest first).
			// We iterate and post history.
			for _, act := range activities {
				if act.UserMessaged != nil && act.UserMessaged.UserMessage != "" {
					msg := fmt.Sprintf("👤 <b>User</b>\n%s", formatTelegramHTML(act.UserMessaged.UserMessage))
					telegramClient.SendMessage(chatID, newThreadID, msg)
				} else if act.AgentMessaged != nil && act.AgentMessaged.AgentMessage != "" {
					msg := formatAgentMessage(act.AgentMessaged.AgentMessage)
					telegramClient.SendMessage(chatID, newThreadID, msg)
				} else if act.PlanGenerated != nil && len(act.PlanGenerated.Plan.Steps) > 0 {
					msg := formatPlan(act)

					sessionIDShort := strings.TrimPrefix(session.Name, "sessions/")
					keyboard := telegram.InlineKeyboardMarkup{
						InlineKeyboard: [][]telegram.InlineKeyboardButton{
							{{Text: "✅ Approve Plan", CallbackData: "approve_plan:" + sessionIDShort}},
						},
					}
					telegramClient.SendMessageWithKeyboard(chatID, newThreadID, msg, keyboard)
				} else if act.SessionCompleted != nil {
					msg := formatCompletionMessage(session)
					sessionIDShort := strings.TrimPrefix(session.Name, "sessions/")

					var inlineButtons [][]telegram.InlineKeyboardButton
					hasPR := false
					var prURL string

					for _, output := range session.Outputs {
						if output.PullRequest != nil && output.PullRequest.URL != "" {
							hasPR = true
							prURL = output.PullRequest.URL
							break
						}
					}

					if hasPR {
						inlineButtons = append(inlineButtons, []telegram.InlineKeyboardButton{
							{Text: "🔗 View Pull Request", URL: prURL},
						})
					} else {
						inlineButtons = append(inlineButtons, []telegram.InlineKeyboardButton{
							{Text: "🔀 Create PR", CallbackData: "create_pr:" + sessionIDShort},
							{Text: "🌿 Create Branch", CallbackData: "create_branch:" + sessionIDShort},
						})
					}
					inlineButtons = append(inlineButtons, []telegram.InlineKeyboardButton{
						{Text: "🔗 Open in Jules", URL: session.URL},
					})

					keyboard := telegram.InlineKeyboardMarkup{InlineKeyboard: inlineButtons}
					telegramClient.SendMessageWithKeyboard(chatID, newThreadID, msg, keyboard)
				} else if act.SessionFailed != nil {
					reason := act.SessionFailed.Reason
					var errMsg string
					if reason != "" {
						errMsg = fmt.Sprintf("⚠️ <b>Jules encountered an error</b>\n\n<blockquote>%s</blockquote>", escapeHTML(reason))
					} else {
						errMsg = "⚠️ <b>Jules encountered an error</b>\n\nThe session failed unexpectedly."
					}
					telegramClient.SendMessage(chatID, newThreadID, errMsg)
				}
			}
		}

		topicKeyboard := telegram.ReplyKeyboardMarkup{
			Keyboard: [][]telegram.KeyboardButton{
				{
					{Text: "🔄 Sync"},
					{Text: "👯 Clone Task"},
					{Text: "📦 Archive Task"},
				},
			},
			ResizeKeyboard: true,
			IsPersistent:   true,
		}

		// Also send the initial user prompt if available
		var userPrompt string
		for _, act := range activities {
			if act.UserMessaged != nil && act.UserMessaged.UserMessage != "" {
				userPrompt = act.UserMessaged.UserMessage
				break
			}
		}

		if userPrompt == "" {
			telegramClient.SendMessageWithReplyKeyboard(chatID, newThreadID, "✅ <b>History Sync Complete!</b> You can now continue this task here.", topicKeyboard)
		} else {
			// Do not send a separate summary message if we just replayed history, just append the keyboard to the last message or a short sync note
			telegramClient.SendMessageWithReplyKeyboard(chatID, newThreadID, "✅ <b>History Sync Complete!</b> You can now continue this task here.", topicKeyboard)
		}

		// Note: The inline keyboard from the `/tasks` command cannot be easily fully re-rendered without a ton of state,
		// but the user can just re-send `/tasks` to see the updated link.
		// For UX, we could edit the original message if we generated it, but `messageID` refers to the inline keyboard message.
		telegramClient.EditMessageText(chatID, messageID, "✅ Topic created and history synced! Please request /tasks again to see the updated link.", nil)

		return
	}

	// Handle Create Branch callback
	if strings.HasPrefix(data, "create_branch:") {
		sessionIDShort := strings.TrimPrefix(data, "create_branch:")
		sessionName := "sessions/" + sessionIDShort

		telegramClient.AnswerCallbackQuery(callbackID, "Sending 'Create Branch' to Jules...")
		if err := julesClient.SendMessage(sessionName, "Create Branch"); err != nil {
			log.Printf("Failed to send Branch request to Jules: %v", err)
			telegramClient.SendMessage(chatID, 0, fmt.Sprintf("❌ Failed to send request: %v", err))
		} else {
			telegramClient.SendMessage(chatID, 0, "🚀 Sent 'Create Branch' command to Jules. Working...")
		}
		return
	}

	if !strings.HasPrefix(data, "switch:") {
		return
	}
	sessionIDShort := strings.TrimPrefix(data, "switch:")
	sessionID := "sessions/" + sessionIDShort

	if err := firestoreClient.UpdateCurrentSession(ctx, chatID, 0, sessionID); err != nil {
		log.Printf("Failed to switch session: %v", err)
		telegramClient.SendMessage(chatID, 0, "Failed to switch session.")
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
	telegramClient.SendMessageWithReplyKeyboard(chatID, 0, fmt.Sprintf("✅ Switched to session <code>%s</code>", sessionIDShort), keyboard)

	// Fetch latest activities to show context
	activities, err := julesClient.ListActivities(sessionID, "")
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
			telegramClient.SendMessage(chatID, 0, formatActivity(*latestJules))
			telegramClient.SendMessage(chatID, 0, formatActivity(*latestUser))
		} else if latestJules != nil {
			telegramClient.SendMessage(chatID, 0, formatActivity(*latestJules))
		} else if latestUser != nil {
			telegramClient.SendMessage(chatID, 0, formatActivity(*latestUser))
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

func formatPlan(act jules.Activity) string {
	title := "📋 Plan Generated"
	if act.PlanGenerated == nil {
		return title
	}
	desc := formatTelegramHTML(act.PlanGenerated.Plan.Title)
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
	return fmt.Sprintf("%s\n%s", title, desc)
}

func formatAgentMessage(text string) string {
	if len(text) > 200 {
		preview := text
		if len(preview) > 100 {
			preview = preview[:100] + "..."
		}
		escapedFull := escapeHTML(text)
		return fmt.Sprintf("💬 <b>Jules</b>\n%s\n<blockquote expandable>%s</blockquote>",
			formatTelegramHTML(preview), escapedFull)
	}
	return fmt.Sprintf("💬 <b>Jules</b>\n%s", formatTelegramHTML(text))
}

func formatCompletionMessage(session *jules.Session) string {
	msg := "✅ <b>Jules has completed the task!</b>\n\n"

	for _, output := range session.Outputs {
		if output.PullRequest != nil {
			msg += fmt.Sprintf("🔀 <b>PR created:</b> <a href=\"%s\">%s</a>\n",
				output.PullRequest.URL, escapeHTML(output.PullRequest.Title))
		}
		if output.ChangeSet != nil && output.ChangeSet.GitPatch.SuggestedCommitMessage != "" {
			commitMsg := output.ChangeSet.GitPatch.SuggestedCommitMessage
			if len(commitMsg) > 200 {
				msg += fmt.Sprintf("📝 <b>Commit message:</b>\n<blockquote expandable>%s</blockquote>",
					escapeHTML(commitMsg))
			} else {
				msg += fmt.Sprintf("📝 <b>Commit message:</b>\n<i>%s</i>", escapeHTML(commitMsg))
			}
		}
	}

	return msg
}

func formatActivity(act jules.Activity) string {
	title := "New Activity"
	desc := ""
	if act.ProgressUpdated != nil && act.ProgressUpdated.Title != "" {
		title = act.ProgressUpdated.Title
		desc = act.ProgressUpdated.Description
	} else if act.PlanGenerated != nil && len(act.PlanGenerated.Plan.Steps) > 0 {
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
		if act.UserMessaged != nil && act.UserMessaged.UserMessage != "" {
			desc = formatTelegramHTML(act.UserMessaged.UserMessage)
		}
	} else if act.Originator == "agent" {
		title = "Jules"
		if act.AgentMessaged != nil && act.AgentMessaged.AgentMessage != "" {
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

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func handleMessage(ctx context.Context, chatID int64, threadID int, text string) {
	if firestoreClient == nil || julesClient == nil || telegramClient == nil {
		log.Println("Clients not initialized")
		return
	}

	chatConfig, err := firestoreClient.GetChatConfig(ctx, chatID, threadID)

	// Intercept Archive/Menu commands from keyboard
	if text == "📦 Archive Chat" || text == "📦 Archive Task" || text == "🏠 Main Menu" || text == "/archive" || text == "🗑 Remove Topic" || text == "🔄 Sync" || text == "👯 Clone Task" {
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
			telegramClient.SendMessageWithReplyKeyboard(chatID, threadID, "Main menu:", keyboard)
			return
		}

		if text == "👯 Clone Task" {
			if chatConfig == nil || chatConfig.CurrentSession == "" {
				telegramClient.SendMessage(chatID, threadID, "No active session to clone.")
				return
			}

			telegramClient.SendMessage(chatID, threadID, "Cloning task...")
			session, err := julesClient.GetSession(chatConfig.CurrentSession)
			if err != nil {
				telegramClient.SendMessage(chatID, threadID, "❌ Failed to retrieve session for cloning.")
				return
			}

			cleanTitle := strings.ReplaceAll(session.Title, "\n", " ")
			runes := []rune(cleanTitle)
			if len(runes) > 40 {
				cleanTitle = string(runes[:37]) + "..."
			}
			newTitle := cleanTitle + " Cloned"

			newThreadID, err := telegramClient.CreateForumTopic(chatID, newTitle)
			if err != nil {
				telegramClient.SendMessage(chatID, threadID, fmt.Sprintf("❌ Failed to create topic: %v", err))
				return
			}

			// Pre-populate configs based on parent
			draftSource := chatConfig.DraftSource
			draftBranch := chatConfig.DraftBranch
			if draftBranch == "" {
				draftBranch = "main"
			}

			firestoreClient.SaveChatConfig(ctx, firestore.ChatConfig{
				ChatID:      chatID,
				ThreadID:    newThreadID,
				DraftSource: draftSource,
				DraftBranch: draftBranch,
				State:       "waiting_for_title",
			})

			var branchButtons [][]telegram.InlineKeyboardButton
			var currentRow []telegram.InlineKeyboardButton
			idx := 1

			// We need to fetch branches for the inherited source
			var branches []string
			sources, _ := julesClient.ListSources()
			for _, s := range sources {
				if s.Name == draftSource {
					for _, b := range s.GithubRepo.Branches {
						branches = append(branches, b.DisplayName)
					}
					break
				}
			}

			if len(branches) == 0 {
				branches = []string{"main"}
			}

			repoPart := draftSource
			sourceParts := strings.Split(draftSource, "/")
			if len(sourceParts) >= 2 {
				repoPart = sourceParts[len(sourceParts)-1]
			}

			msgBuilder := fmt.Sprintf("✏️ <b>Repository:</b> %s\n🌿 <b>Base branch:</b> %s\nTask cloned! Select another base branch below, or reply with a new title for this task:\n\n", repoPart, draftBranch)

			for _, branch := range branches {
				btnText := fmt.Sprintf("%d", idx)
				if branch == draftBranch {
					btnText = "✅ " + btnText
				}
				btn := telegram.InlineKeyboardButton{
					Text:         btnText,
					CallbackData: fmt.Sprintf("topicbranch:%d:%s", newThreadID, branch),
				}
				currentRow = append(currentRow, btn)
				msgBuilder += fmt.Sprintf("<b>%d</b> — <code>%s</code>\n", idx, branch)

				if len(currentRow) == 5 {
					branchButtons = append(branchButtons, currentRow)
					currentRow = nil
				}
				idx++
			}
			if len(currentRow) > 0 {
				branchButtons = append(branchButtons, currentRow)
			}

			topicKeyboard := telegram.InlineKeyboardMarkup{InlineKeyboard: branchButtons}
			telegramClient.SendMessageWithKeyboard(chatID, newThreadID, msgBuilder, topicKeyboard)
			return
		}

		if text == "🔄 Sync" {
			telegramClient.SendMessage(chatID, threadID, "⏳ Poller will sync updates shortly.")
			// In a real implementation we could trigger a pubsub message to the poller here.
			// For now, we just reply to give user feedback.
			return
		}

		if text == "🗑 Remove Topic" {
			if threadID > 0 {
				telegramClient.DeleteForumTopic(chatID, threadID)
				firestoreClient.DeleteChatConfig(ctx, chatID, threadID)
			} else {
				telegramClient.SendMessage(chatID, threadID, "This action is only available in topics.")
			}
			return
		}

		if text == "📦 Archive Chat" || text == "📦 Archive Task" || text == "/archive" {
			if chatConfig == nil || chatConfig.CurrentSession == "" {
				telegramClient.SendMessage(chatID, threadID, "No active session to archive.")
				return
			}

			telegramClient.SendMessage(chatID, threadID, "⏳ Archiving session...")
			if err := julesClient.ArchiveSession(chatConfig.CurrentSession); err != nil {
				log.Printf("Failed to archive session: %v", err)
				telegramClient.SendMessage(chatID, threadID, fmt.Sprintf("Failed to archive session: %v", err))
				return
			}

			// Clear current session in firestore
			firestoreClient.UpdateCurrentSession(ctx, chatID, threadID, "")

			if threadID > 0 {
				telegramClient.DeleteForumTopic(chatID, threadID)
				firestoreClient.DeleteChatConfig(ctx, chatID, threadID)
			} else {
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
				telegramClient.SendMessageWithReplyKeyboard(chatID, threadID, "✅ Session archived successfully.", keyboard)
			}
			return
		}
		return
	}

	// Intercept Prev/Next commands from keyboard (Deprecating but keeping code block structure for a moment if needed, actually user said remove it)
	if text == "⬅️ Prev Task" || text == "Next Task ➡️" {
		telegramClient.SendMessage(chatID, threadID, "Previous/Next navigation is disabled. Use 'Show Tasks' to switch.")
		return
	}

	// Handle setting topic title for cloned tasks
	if err == nil && chatConfig.State == "waiting_for_title" {
		if text != "/skip" && threadID > 0 {
			// Actually telegram API doesn't have an endpoint specifically named `editForumTopic` easily mapped.
			// It has `editForumTopic` with `chat_id`, `message_thread_id`, `name`.
			// Let's add EditForumTopic to TelegramClient interface.
			telegramClient.EditForumTopic(chatID, threadID, text)
		}

		if err := firestoreClient.UpdateChatState(ctx, chatID, threadID, "waiting_for_message", chatConfig.DraftSource); err != nil {
			log.Printf("Failed to update chat state: %v", err)
		}
		telegramClient.SendMessage(chatID, threadID, "✅ Title set. Now, please enter the initial message for this new task:")
		return
	}

	// Handle creation flow
	if err == nil && chatConfig.State == "waiting_for_message" {
		telegramClient.SendMessage(chatID, threadID, "⏳ Creating session on Jules...")

		session, err := julesClient.CreateSession(text, chatConfig.DraftSource, chatConfig.CreationMode, chatConfig.DraftBranch)
		if err != nil {
			telegramClient.SendMessage(chatID, threadID, fmt.Sprintf("Failed to create session: %v", err))
			firestoreClient.UpdateChatState(ctx, chatID, threadID, "", "")
			return
		}

		// Switch current session to this new session
		firestoreClient.UpdateCurrentSession(ctx, chatID, threadID, session.Name)
		firestoreClient.UpdateChatState(ctx, chatID, threadID, "", "")

		if threadID > 0 {
			topicKeyboard := telegram.ReplyKeyboardMarkup{
				Keyboard: [][]telegram.KeyboardButton{
					{
						{Text: "🔄 Sync"},
						{Text: "👯 Clone Task"},
						{Text: "📦 Archive Task"},
					},
				},
				ResizeKeyboard: true,
				IsPersistent:   true,
			}
			telegramClient.SendMessageWithReplyKeyboard(chatID, threadID, fmt.Sprintf("✅ <b>New Chat Created!</b>\nSwitched session to: <code>%s</code>\n", strings.TrimPrefix(session.Name, "sessions/")), topicKeyboard)
		} else {
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
			telegramClient.SendMessageWithReplyKeyboard(chatID, threadID, fmt.Sprintf("✅ <b>New Chat Created!</b>\nSwitched session to: <code>%s</code>\n", strings.TrimPrefix(session.Name, "sessions/")), keyboard)
		}
		return
	}

	if err != nil || chatConfig.CurrentSession == "" {
		telegramClient.SendMessage(chatID, threadID, "No active session. Use /tasks to select one or /new_chat to create one.")
		return
	}

	if err := julesClient.SendMessage(chatConfig.CurrentSession, text); err != nil {
		log.Printf("Failed to send message to Jules: %v", err)
		telegramClient.SendMessage(chatID, threadID, "Failed to send message to Jules.")
		return
	}

	// Reset progress message ID so a new log block starts after user input
	firestoreClient.UpdateProgressMessageID(ctx, chatID, threadID, 0)
}
