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
	"github.com/neverknowerdev/jules-telegram-bot/internal/telegraph"
)

var (
	julesClient     jules.ClientInterface
	firestoreClient firestore.ClientInterface
	telegramClient  telegram.ClientInterface
	telegraphClient *telegraph.Client
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

	telegraphToken := os.Getenv("TELEGRAPH_ACCESS_TOKEN")
	if telegraphToken != "" {
		telegraphClient = telegraph.NewClient(telegraphToken)
	}
}

func buildTelegraphContent(lines []string) []telegraph.Node {
	var nodes []telegraph.Node
	for _, line := range lines {
		// Clean up HTML tags from line
		line = strings.ReplaceAll(line, "<b>", "")
		line = strings.ReplaceAll(line, "</b>", "")
		line = strings.ReplaceAll(line, "<i>", "")
		line = strings.ReplaceAll(line, "</i>", "")
		line = strings.ReplaceAll(line, "<pre>", "")
		line = strings.ReplaceAll(line, "</pre>", "")
		line = strings.ReplaceAll(line, "<code>", "")
		line = strings.ReplaceAll(line, "</code>", "")
		line = strings.ReplaceAll(line, "<blockquote expandable>", "")
		line = strings.ReplaceAll(line, "</blockquote>", "")

		nodes = append(nodes, telegraph.Node{
			Tag:      "p",
			Children: []telegraph.NodeChild{line},
		})
	}
	if len(nodes) == 0 {
		nodes = append(nodes, telegraph.Node{
			Tag:      "p",
			Children: []telegraph.NodeChild{"No logs available yet."},
		})
	}
	return nodes
}

func JulesPoller(w http.ResponseWriter, r *http.Request) {
	if projectID == "" {
		initEnv()
	}
	ctx := context.Background()

	if firestoreClient == nil {
		realFirestoreClient, err := firestore.NewClient(ctx, projectID)
		if err != nil {
			log.Printf("Failed to create Firestore client: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		firestoreClient = realFirestoreClient
	}

	// 1. Process all chats from Firestore using iterator to save memory
	err := firestoreClient.IterateAllChats(ctx, func(chat firestore.ChatConfig) error {
		if chat.CurrentSession == "" {
			return nil
		}

		log.Printf("[POLLER] Processing chat %d", chat.ChatID)

		// Defer a recover within the loop to catch panics per chat
		defer func() {
			if r := recover(); r != nil {
				errPart := fmt.Sprintf("🚨 <b>Technical Error in Poller</b>\n\n<blockquote>%v</blockquote>", r)
				if telegramClient != nil {
					telegramClient.SendMessage(chat.ChatID, chat.ThreadID, errPart)
				}
				log.Printf("[POLLER] Panic in chat %d: %v", chat.ChatID, r)
			}
		}()

		// Fetch current session details from Jules
		session, err := julesClient.GetSession(chat.CurrentSession)
		if err != nil {
			log.Printf("[POLLER] Chat %d: failed to get session: %v", chat.ChatID, err)
			return nil
		}

		// 2. Detect session state transitions (e.g. COMPLETED -> IN_PROGRESS)
		isTransitioningToActive := (chat.State == "COMPLETED" || chat.State == "FAILED") &&
			(session.State == "IN_PROGRESS" || session.State == "PLANNING" || session.State == "AWAITING_PLAN_APPROVAL")

		if isTransitioningToActive {
			log.Printf("[POLLER] Chat %d: session re-activated (%s -> %s), resetting tracking", chat.ChatID, chat.State, session.State)
			firestoreClient.UpdateProgressMessageID(ctx, chat.ChatID, chat.ThreadID, 0)
			chat.ProgressMessageID = 0
		}

		// Update tracked state in Firestore if changed
		if chat.State != session.State {
			firestoreClient.UpdateChatState(ctx, chat.ChatID, chat.ThreadID, session.State, chat.DraftSource)
			chat.State = session.State
		}

		activities, err := julesClient.ListActivities(chat.CurrentSession, chat.LastActivityID)
		if err != nil {
			log.Printf("[POLLER] Failed to list activities for chat %d (Thread %d): %v", chat.ChatID, chat.ThreadID, err)
			return nil
		}

		if len(activities) == 0 {
			return nil
		}

		log.Printf("[POLLER] Chat %d (Thread %d): fetched %d NEW activities from Jules (LastActivityID was %q)", chat.ChatID, chat.ThreadID, len(activities), chat.LastActivityID)

		newestID := activities[len(activities)-1].Id

		// First run for this session: just set cursor to newest and skip sending.
		if chat.LastActivityID == "" {
			log.Printf("[POLLER] Chat %d (Thread %d): first run, marking newest activity %q and skipping", chat.ChatID, chat.ThreadID, newestID)
			firestoreClient.UpdateLastActivity(ctx, chat.ChatID, chat.ThreadID, newestID)
			return nil
		}

		var hasNewProgress bool = isTransitioningToActive || (chat.ProgressMessageID == 0 && (session.State == "IN_PROGRESS" || session.State == "PLANNING"))
		progressMsgID := chat.ProgressMessageID

		// Process actual NEW activities
		for i, act := range activities {
			if act.Originator == "user" {
				continue
			}

			if act.PlanGenerated != nil && len(act.PlanGenerated.Plan.Steps) > 0 {
				// If this is the LATEST plan and session is AWAITING_PLAN_APPROVAL, it requires approval.
				isLatestPlan := true
				for j := i + 1; j < len(activities); j++ {
					if activities[j].PlanGenerated != nil {
						isLatestPlan = false
						break
					}
				}

				if isLatestPlan && session.State == "AWAITING_PLAN_APPROVAL" {
					log.Printf("[POLLER] Chat %d (Thread %d): sending approval plan %s", chat.ChatID, chat.ThreadID, act.Id)
					progressMsgID = 0
					firestoreClient.UpdateProgressMessageID(ctx, chat.ChatID, chat.ThreadID, 0)
					msg := formatPlan(act)
					sessionIDShort := strings.TrimPrefix(chat.CurrentSession, "sessions/")
					keyboard := telegram.InlineKeyboardMarkup{
						InlineKeyboard: [][]telegram.InlineKeyboardButton{
							{{Text: "✅ Approve Plan", CallbackData: "approve_plan:" + sessionIDShort}},
						},
					}
					telegramClient.SendMessageWithKeyboard(chat.ChatID, chat.ThreadID, msg, keyboard)
				} else {
					// It's an execution plan (auto-approved) or an old plan. Treat it as progress.
					hasNewProgress = true
				}
				continue
			}

			if act.AgentMessaged != nil && act.AgentMessaged.AgentMessage != "" {
				msg := formatAgentMessage(act.AgentMessaged.AgentMessage)
				telegramClient.SendMessage(chat.ChatID, chat.ThreadID, msg)
				continue
			}

			// Session failed activity — send error notification immediately
			if act.SessionFailed != nil {
				reason := act.SessionFailed.Reason
				var errMsg string
				if reason != "" {
					errMsg = fmt.Sprintf("⚠️ <b>Jules encountered an error</b>\n\n<blockquote>%s</blockquote>", escapeHTML(reason))
				} else {
					errMsg = "⚠️ <b>Jules encountered an error</b>\n\nThe session failed unexpectedly."
				}
				telegramClient.SendMessage(chat.ChatID, chat.ThreadID, errMsg)
				continue
			}

			// Session completed activity — send completion notification immediately
			if act.SessionCompleted != nil {
				msg := formatCompletionMessage(session)
				sessionIDShort := strings.TrimPrefix(chat.CurrentSession, "sessions/")

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
					{Text: "👯 Clone Task", CallbackData: "clone:" + sessionIDShort},
				})

				keyboard := telegram.InlineKeyboardMarkup{InlineKeyboard: inlineButtons}
				telegramClient.SendMessageWithKeyboard(chat.ChatID, chat.ThreadID, msg, keyboard)
				continue
			}

			if (act.ProgressUpdated != nil && act.ProgressUpdated.Title != "") || len(act.Artifacts) > 0 {
				hasNewProgress = true
			}
		}

		firestoreClient.UpdateLastActivity(ctx, chat.ChatID, chat.ThreadID, newestID)

		// Rebuild and update the progress message if needed
		if hasNewProgress {
			var allProgressLines []string
			for _, act := range activities {
				if act.Originator == "user" {
					continue
				}
				ts := formatTimestamp(act.CreateTime)

				// Re-evaluate if this plan is an execution plan
				isExecutionPlan := false
				if act.PlanGenerated != nil && len(act.PlanGenerated.Plan.Steps) > 0 {
					// Check if it's the latest plan AND requires approval
					isLatestPlan := true
					for j := 0; j < len(activities); j++ {
						if activities[j].PlanGenerated != nil && activities[j].CreateTime > act.CreateTime {
							isLatestPlan = false
							break
						}
					}
					requiresApproval := isLatestPlan && session.State == "AWAITING_PLAN_APPROVAL"
					isExecutionPlan = !requiresApproval
				}

				if isExecutionPlan {
					if line := formatExecutionPlanLine(act, ts); line != "" {
						allProgressLines = append(allProgressLines, line)
					}
				} else if act.PlanGenerated == nil {
					// Only format standard progress lines if it's not a PlanGenerated
					if line := formatProgressLine(act, ts); line != "" {
						allProgressLines = append(allProgressLines, line)
					}
				}
			}

			if len(allProgressLines) > 0 || progressMsgID == 0 {
				header := "⚙️ <b>Jules is working on it...</b>\n\n"

				// Sync to Telegraph
				var telegraphURL string
				if telegraphClient != nil && telegraphClient.AccessToken != "" {
					pageContent := buildTelegraphContent(allProgressLines)
					pageTitle := "Jules Task Logs"

					var currentTelegraphPath string
					if len(chat.TelegraphPages) > 0 {
						currentTelegraphPath = chat.TelegraphPages[len(chat.TelegraphPages)-1]
					}

					if currentTelegraphPath == "" {
						pageResp, err := telegraphClient.CreatePage(pageTitle, pageContent)
						if err == nil {
							telegraphURL = pageResp.Result.URL
							chat.TelegraphPages = append(chat.TelegraphPages, pageResp.Result.Path)
							firestoreClient.AppendTelegraphPage(ctx, chat.ChatID, chat.ThreadID, pageResp.Result.Path)
						} else {
							log.Printf("[POLLER] Telegraph CreatePage failed: %v", err)
						}
					} else {
						pageResp, err := telegraphClient.EditPage(currentTelegraphPath, pageTitle, pageContent)
						if err == nil {
							telegraphURL = pageResp.Result.URL
						} else {
							log.Printf("[POLLER] Telegraph EditPage failed: %v", err)
						}
					}
				}

				// Pack as many as we can fit within ~4000 characters
				var finalLines []string
				currentLen := len(header)
				omittedCount := 0

				for i := len(allProgressLines) - 1; i >= 0; i-- {
					lineLen := len(allProgressLines[i]) + 2 // +2 for the \n\n
					if currentLen+lineLen > 4000 {
						omittedCount = i + 1
						finalLines = append([]string{fmt.Sprintf("<i>...%d older steps omitted...</i>", omittedCount)}, finalLines...)
						break
					}
					finalLines = append([]string{allProgressLines[i]}, finalLines...)
					currentLen += lineLen
				}

				body := strings.Join(finalLines, "\n\n")
				var msg string
				if body == "" {
					msg = strings.TrimSpace(header)
				} else {
					msg = header + body
				}

				var keyboard *telegram.InlineKeyboardMarkup
				if telegraphURL != "" {
					keyboard = &telegram.InlineKeyboardMarkup{
						InlineKeyboard: [][]telegram.InlineKeyboardButton{
							{{Text: "🔗 Full log", URL: telegraphURL}},
						},
					}
				}

				if progressMsgID == 0 {
					var err error
					var msgID int
					if keyboard != nil {
						msgID, err = telegramClient.SendMessageWithKeyboardReturningID(chat.ChatID, chat.ThreadID, msg, *keyboard)
					} else {
						msgID, err = telegramClient.SendMessageReturningID(chat.ChatID, chat.ThreadID, msg)
					}

					if err == nil {
						firestoreClient.UpdateProgressMessageID(ctx, chat.ChatID, chat.ThreadID, msgID)
						chat.ProgressMessageID = msgID
					} else {
						log.Printf("[POLLER] Chat %d (Thread %d): FAILED to create progress message: %v", chat.ChatID, chat.ThreadID, err)
					}
				} else {
					editErr := telegramClient.EditMessageText(chat.ChatID, progressMsgID, msg, keyboard)
					if editErr != nil {
						log.Printf("[POLLER] Chat %d (Thread %d): edit failed: %v", chat.ChatID, chat.ThreadID, editErr)
						var msgID int
						var err error
						if keyboard != nil {
							msgID, err = telegramClient.SendMessageWithKeyboardReturningID(chat.ChatID, chat.ThreadID, msg, *keyboard)
						} else {
							msgID, err = telegramClient.SendMessageReturningID(chat.ChatID, chat.ThreadID, msg)
						}

						if err == nil {
							firestoreClient.UpdateProgressMessageID(ctx, chat.ChatID, chat.ThreadID, msgID)
							chat.ProgressMessageID = msgID
						} else {
							log.Printf("[POLLER] Chat %d (Thread %d): FAILED to create replacement progress message: %v", chat.ChatID, chat.ThreadID, err)
						}
					}
				}
			}
		}

		// 3. Detect new PRs or Branches in outputs
		for _, output := range session.Outputs {
			if output.PullRequest != nil {
				prURL := output.PullRequest.URL
				if !chat.NotifiedPRs[prURL] {
					msg := fmt.Sprintf("🔀 <b>New Pull Request Created!</b>\n\n<b>Title:</b> %s\n<b>Branch:</b> <code>%s</code> → <code>%s</code>",
						escapeHTML(output.PullRequest.Title), escapeHTML(output.PullRequest.HeadRef), escapeHTML(output.PullRequest.BaseRef))
					keyboard := telegram.InlineKeyboardMarkup{
						InlineKeyboard: [][]telegram.InlineKeyboardButton{{{Text: "🔗 View Pull Request", URL: prURL}}},
					}

					telegramClient.UnpinAllChatMessages(chat.ChatID, chat.ThreadID)

					if msgID, err := telegramClient.SendMessageWithKeyboardReturningID(chat.ChatID, chat.ThreadID, msg, keyboard); err == nil {
						telegramClient.PinChatMessage(chat.ChatID, chat.ThreadID, msgID)
						firestoreClient.MarkPRAsNotified(ctx, chat.ChatID, chat.ThreadID, prURL)
						// Update the chat object in memory for subsequent checks within the same poller run
						if chat.NotifiedPRs == nil {
							chat.NotifiedPRs = make(map[string]bool)
						}
						chat.NotifiedPRs[prURL] = true
					}
				}
			}
			if output.ChangeSet != nil && output.ChangeSet.Source != "" {
				source := output.ChangeSet.Source
				parts := strings.Split(source, "/branches/")
				if len(parts) > 1 {
					branchName := parts[1]
					if !chat.NotifiedBranches[branchName] {
						msg := fmt.Sprintf("🌿 <b>New GitHub Branch Created!</b>\n\n<b>Branch:</b> <code>%s</code>", escapeHTML(branchName))
						if err := telegramClient.SendMessage(chat.ChatID, chat.ThreadID, msg); err == nil {
							firestoreClient.MarkBranchAsNotified(ctx, chat.ChatID, chat.ThreadID, branchName)
							// Update the chat object in memory for subsequent checks within the same poller run
							if chat.NotifiedBranches == nil {
								chat.NotifiedBranches = make(map[string]bool)
							}
							chat.NotifiedBranches[branchName] = true
						}
					}
				}
			}
		}
		return nil
	})

	if err != nil {
		log.Printf("Error iterating chats: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
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
	// If the message is long, wrap it in an expandable blockquote
	if len(text) > 200 {
		preview := text
		if len(preview) > 100 {
			preview = preview[:100] + "..."
		}
		// Use plain text inside blockquote to avoid HTML nesting issues
		escapedFull := escapeHTML(text)
		return fmt.Sprintf("💬 <b>Jules</b>\n%s\n<blockquote expandable>%s</blockquote>",
			formatTelegramHTML(preview), escapedFull)
	}
	return fmt.Sprintf("💬 <b>Jules</b>\n%s", formatTelegramHTML(text))
}

func formatExecutionPlanLine(act jules.Activity, ts string) string {
	if act.PlanGenerated == nil {
		return ""
	}
	stepCount := len(act.PlanGenerated.Plan.Steps)
	planTitle := act.PlanGenerated.Plan.Title
	if planTitle == "" {
		planTitle = "Updated plan"
	}
	planTitle = escapeHTML(planTitle)

	// Build step list as plain text
	var steps strings.Builder
	for i, step := range act.PlanGenerated.Plan.Steps {
		steps.WriteString(fmt.Sprintf("%d. %s\n", i+1, escapeHTML(step.Title)))
	}

	return fmt.Sprintf("[%s] 📋 <b>%s</b> (%d steps)\n<blockquote expandable>%s</blockquote>",
		ts, planTitle, stepCount, strings.TrimSpace(steps.String()))
}

func formatProgressLine(act jules.Activity, ts string) string {
	if act.ProgressUpdated != nil && act.ProgressUpdated.Title != "" {
		title := formatTelegramHTML(act.ProgressUpdated.Title)
		if act.ProgressUpdated.Description != "" {
			descText := act.ProgressUpdated.Description
			if len(descText) > 400 {
				descText = descText[:400] + "..."
			}
			if len(descText) > 120 {
				// Use expandable blockquote — plain text inside to avoid nesting issues
				return fmt.Sprintf("[%s] ✅ <b>%s</b>\n<blockquote expandable>%s</blockquote>", ts, title, escapeHTML(descText))
			}
			desc := formatTelegramHTML(descText)
			return fmt.Sprintf("[%s] ✅ <b>%s</b>\n<i>%s</i>", ts, title, desc)
		}
		return fmt.Sprintf("[%s] ✅ <b>%s</b>", ts, title)
	}

	if len(act.Artifacts) > 0 {
		return fmt.Sprintf("[%s] 📝 Working...", ts)
	}

	return ""
}

func formatCompletionMessage(session *jules.Session) string {
	msg := "✅ <b>Jules has completed the task!</b>\n\n"

	// Add commit message from outputs
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

// escapeHTML escapes &, <, > for use in Telegram HTML without any markdown conversion.
func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func formatTimestamp(createTime string) string {
	// createTime format: "2026-03-04T20:19:31.780461Z"
	if len(createTime) >= 16 {
		return createTime[11:16] // "HH:MM"
	}
	return "??:??"
}

func trimProgressMessage(msg string) string {
	// Not used anymore due to array slicing. Returning untouched text.
	return msg
}

func formatTelegramHTML(md string) string {
	// First escape <, >, & so they don't break HTML parsing
	md = strings.ReplaceAll(md, "&", "&amp;")
	md = strings.ReplaceAll(md, "<", "&lt;")
	md = strings.ReplaceAll(md, ">", "&gt;")

	// markdown to HTML conversions
	// **bold**
	reBold := regexp.MustCompile(`\*\*(.*?)\*\*`)
	md = reBold.ReplaceAllString(md, "<b>$1</b>")

	// *italic*
	reItalic := regexp.MustCompile(`\*(.*?)\*`)
	md = reItalic.ReplaceAllString(md, "<i>$1</i>")

	// ```code block```
	reCodeBlock := regexp.MustCompile("(?s)```(.*?)```")
	md = reCodeBlock.ReplaceAllString(md, "<pre>$1</pre>")

	// `inline code`
	reInline := regexp.MustCompile("(?s)`(.*?)`")
	md = reInline.ReplaceAllString(md, "<code>$1</code>")

	return md
}
