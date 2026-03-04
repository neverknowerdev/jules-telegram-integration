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

	log.Printf("[POLLER] Found %d chats", len(chats))

	for _, chat := range chats {
		log.Printf("[POLLER] Chat %d: session=%q lastActivity=%q", chat.ChatID, chat.CurrentSession, chat.LastActivityID)
		if chat.CurrentSession == "" {
			log.Printf("[POLLER] Chat %d: no current session, skipping", chat.ChatID)
			continue
		}

		activities, err := julesClient.ListActivities(chat.CurrentSession)
		if err != nil {
			log.Printf("Failed to list activities for chat %d: %v", chat.ChatID, err)
			continue
		}

		log.Printf("[POLLER] Chat %d: fetched %d activities from Jules (oldest=%q newest=%q)",
			chat.ChatID, len(activities), activities[0].Id, activities[len(activities)-1].Id)

		// Activities are returned OLDEST-FIRST by the Jules API.
		// We find the last-seen activity and collect everything AFTER it (newer).
		newestID := activities[len(activities)-1].Id

		// Log last 3 activities (newest end) for debugging
		for i := max(0, len(activities)-3); i < len(activities); i++ {
			act := activities[i]
			hasPlan := len(act.PlanGenerated.Plan.Steps) > 0
			log.Printf("[POLLER] Chat %d: activity[%d/%d] id=%q originator=%q hasPlan=%v hasProgress=%v",
				chat.ChatID, i, len(activities)-1, act.Id, act.Originator, hasPlan, act.ProgressUpdated.Title != "")
		}

		// First run for this session: just set cursor to newest and skip sending.
		if chat.LastActivityID == "" {
			log.Printf("[POLLER] Chat %d: first run, marking newest activity %q and skipping", chat.ChatID, newestID)
			firestoreClient.UpdateLastActivity(ctx, chat.ChatID, newestID)
			continue
		}

		// Find the index of the last-seen activity, collect everything after it.
		var newActivities []jules.Activity
		foundLast := false
		for i, act := range activities {
			if act.Id == chat.LastActivityID {
				foundLast = true
				// Everything after this index is new (older index = older activity)
				newActivities = activities[i+1:]
				break
			}
		}

		log.Printf("[POLLER] Chat %d: foundLast=%v newActivities=%d", chat.ChatID, foundLast, len(newActivities))

		if !foundLast {
			// Cursor not found — session may have changed or list grew past page size.
			// Send up to 5 newest to avoid spam.
			log.Printf("[POLLER] Chat %d: lastActivityID not found in list, sending up to 5 newest", chat.ChatID)
			start := max(0, len(activities)-5)
			newActivities = activities[start:]
		}

		if len(newActivities) == 0 {
			log.Printf("[POLLER] Chat %d: no new activities to send", chat.ChatID)
		} else {
			// Determine which plans are "approval plans" vs "execution plans".
			// An execution plan appears AFTER work has already started (after progressUpdated activities).
			// We check the FULL activities list to see if work started before this plan.
			isWorkStarted := func(planIdx int) bool {
				for i := 0; i < planIdx; i++ {
					if activities[i].ProgressUpdated.Title != "" || len(activities[i].Artifacts) > 0 {
						return true
					}
				}
				return false
			}

			// Build a set of activity IDs that are execution plans
			executionPlans := make(map[string]bool)
			for i, act := range activities {
				if len(act.PlanGenerated.Plan.Steps) > 0 && isWorkStarted(i) {
					executionPlans[act.Id] = true
				}
			}

			// Separate activities into categories:
			// - Approval Plans: separate messages with Approve button
			// - Agent text messages: separate messages
			// - Progress updates + execution plans: grouped into one editable message
			var hasNewProgress bool
			progressMsgID := chat.ProgressMessageID

			for _, act := range newActivities {
				if act.Originator == "user" {
					continue
				}

				// Plans
				if len(act.PlanGenerated.Plan.Steps) > 0 {
					if executionPlans[act.Id] {
						log.Printf("[POLLER] Chat %d: execution plan (during work), will include in progress log", chat.ChatID)
						hasNewProgress = true
					} else {
						log.Printf("[POLLER] Chat %d: sending approval plan with %d steps", chat.ChatID, len(act.PlanGenerated.Plan.Steps))
						progressMsgID = 0
						firestoreClient.UpdateProgressMessageID(ctx, chat.ChatID, 0)

						msg := formatPlan(act)
						sessionIDShort := strings.TrimPrefix(chat.CurrentSession, "sessions/")
						keyboard := telegram.InlineKeyboardMarkup{
							InlineKeyboard: [][]telegram.InlineKeyboardButton{
								{
									{Text: "✅ Approve Plan", CallbackData: "approve_plan:" + sessionIDShort},
								},
							},
						}
						if err := telegramClient.SendMessageWithKeyboard(chat.ChatID, msg, keyboard); err != nil {
							log.Printf("[POLLER] Chat %d: FAILED to send plan: %v", chat.ChatID, err)
						}
					}
					continue
				}

				// Agent text messages → separate message
				if act.AgentMessaged.AgentMessage != "" {
					log.Printf("[POLLER] Chat %d: sending agent message", chat.ChatID)
					msg := formatAgentMessage(act.AgentMessaged.AgentMessage)
					if err := telegramClient.SendMessage(chat.ChatID, msg); err != nil {
						log.Printf("[POLLER] Chat %d: FAILED to send agent message: %v", chat.ChatID, err)
					}
					continue
				}

				// Progress updates → flag that we have new progress
				if act.ProgressUpdated.Title != "" || len(act.Artifacts) > 0 {
					hasNewProgress = true
				}
			}

			// If we have new progress, rebuild progress message from ALL activities since last approved plan.
			if hasNewProgress {
				lastApprovedPlanIdx := -1
				for i := len(activities) - 1; i >= 0; i-- {
					if len(activities[i].PlanGenerated.Plan.Steps) > 0 && !executionPlans[activities[i].Id] {
						lastApprovedPlanIdx = i
						break
					}
				}

				var allProgressLines []string
				for i := lastApprovedPlanIdx + 1; i < len(activities); i++ {
					act := activities[i]
					if act.Originator == "user" {
						continue
					}
					ts := formatTimestamp(act.CreateTime)

					if len(act.PlanGenerated.Plan.Steps) > 0 && executionPlans[act.Id] {
						line := formatExecutionPlanLine(act, ts)
						if line != "" {
							allProgressLines = append(allProgressLines, line)
						}
						continue
					}

					line := formatProgressLine(act, ts)
					if line != "" {
						allProgressLines = append(allProgressLines, line)
					}
				}

				log.Printf("[POLLER] Chat %d: %d total progress lines, progressMsgID=%d", chat.ChatID, len(allProgressLines), progressMsgID)

				if len(allProgressLines) > 0 {
					header := "⚙️ <b>Jules is working on it...</b>\n\n"
					body := strings.Join(allProgressLines, "\n")
					msg := trimProgressMessage(header + body)

					if progressMsgID == 0 {
						msgID, err := telegramClient.SendMessageReturningID(chat.ChatID, msg)
						if err != nil {
							log.Printf("[POLLER] Chat %d: FAILED to create progress message: %v", chat.ChatID, err)
						} else {
							firestoreClient.UpdateProgressMessageID(ctx, chat.ChatID, msgID)
							log.Printf("[POLLER] Chat %d: created progress message %d", chat.ChatID, msgID)
						}
					} else {
						if err := telegramClient.EditMessageText(chat.ChatID, progressMsgID, msg, nil); err != nil {
							log.Printf("[POLLER] Chat %d: FAILED to edit progress message %d: %v (creating new one)", chat.ChatID, progressMsgID, err)
							msgID, err := telegramClient.SendMessageReturningID(chat.ChatID, msg)
							if err != nil {
								log.Printf("[POLLER] Chat %d: FAILED to create replacement progress message: %v", chat.ChatID, err)
							} else {
								firestoreClient.UpdateProgressMessageID(ctx, chat.ChatID, msgID)
							}
						}
					}
				}
			}

			// Update cursor to the newest activity (last in list).
			log.Printf("[POLLER] Chat %d: updating LastActivityID to newest %q", chat.ChatID, newestID)
			firestoreClient.UpdateLastActivity(ctx, chat.ChatID, newestID)
		}

		// Always fetch current session details to check for state and outputs (PRs/Branches)
		session, err := julesClient.GetSession(chat.CurrentSession)
		if err != nil {
			log.Printf("[POLLER] Chat %d: failed to get session: %v", chat.ChatID, err)
		} else {
			// 1. Detect new PRs or Branches in outputs
			for _, output := range session.Outputs {
				// PR Check
				if output.PullRequest != nil {
					prURL := output.PullRequest.URL
					if !chat.NotifiedPRs[prURL] {
						log.Printf("[POLLER] Chat %d: detected NEW PR %q", chat.ChatID, prURL)
						msg := fmt.Sprintf("🔀 <b>New Pull Request Created!</b>\n\n<b>Title:</b> %s\n<b>Branch:</b> <code>%s</code> → <code>%s</code>",
							escapeHTML(output.PullRequest.Title), escapeHTML(output.PullRequest.HeadRef), escapeHTML(output.PullRequest.BaseRef))
						keyboard := telegram.InlineKeyboardMarkup{
							InlineKeyboard: [][]telegram.InlineKeyboardButton{
								{{Text: "🔗 View Pull Request", URL: prURL}},
							},
						}
						if err := telegramClient.SendMessageWithKeyboard(chat.ChatID, msg, keyboard); err == nil {
							firestoreClient.MarkPRAsNotified(ctx, chat.ChatID, prURL)
							// Update local map to avoid double trigger in same run
							if chat.NotifiedPRs == nil {
								chat.NotifiedPRs = make(map[string]bool)
							}
							chat.NotifiedPRs[prURL] = true
						}
					}
				}

				// Branch Check
				if output.ChangeSet != nil && output.ChangeSet.Source != "" {
					source := output.ChangeSet.Source
					// Simple heuristic: if source contains "/branches/", extract the part after it
					parts := strings.Split(source, "/branches/")
					if len(parts) > 1 {
						branchName := parts[1]
						if !chat.NotifiedBranches[branchName] {
							log.Printf("[POLLER] Chat %d: detected NEW Branch %q", chat.ChatID, branchName)
							msg := fmt.Sprintf("🌿 <b>New GitHub Branch Created!</b>\n\n<b>Branch:</b> <code>%s</code>", escapeHTML(branchName))
							if err := telegramClient.SendMessage(chat.ChatID, msg); err == nil {
								firestoreClient.MarkBranchAsNotified(ctx, chat.ChatID, branchName)
								// Update local map
								if chat.NotifiedBranches == nil {
									chat.NotifiedBranches = make(map[string]bool)
								}
								chat.NotifiedBranches[branchName] = true
							}
						}
					}
				}
			}

			// 2. Original Completion/Failure Logic
			if !chat.CompletionSent {
				if session.State == "COMPLETED" {
					log.Printf("[POLLER] Chat %d: session COMPLETED", chat.ChatID)
					msg := formatCompletionMessage(session)
					sessionIDShort := strings.TrimPrefix(chat.CurrentSession, "sessions/")
					keyboard := telegram.InlineKeyboardMarkup{
						InlineKeyboard: [][]telegram.InlineKeyboardButton{
							{
								{Text: "🔀 Create PR", CallbackData: "create_pr:" + sessionIDShort},
								{Text: "🌿 Create Branch", CallbackData: "create_branch:" + sessionIDShort},
							},
							{
								{Text: "🔗 Open in Jules", URL: session.URL},
							},
						},
					}
					if err := telegramClient.SendMessageWithKeyboard(chat.ChatID, msg, keyboard); err == nil {
						firestoreClient.SetCompletionSent(ctx, chat.ChatID, true)
						chat.CompletionSent = true
					}
				} else if session.State == "FAILED" {
					log.Printf("[POLLER] Chat %d: session FAILED", chat.ChatID)
					if err := telegramClient.SendMessage(chat.ChatID, "❌ <b>Jules task failed</b>\nThe session encountered an error."); err == nil {
						firestoreClient.SetCompletionSent(ctx, chat.ChatID, true)
						chat.CompletionSent = true
					}
				}
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}

func formatPlan(act jules.Activity) string {
	title := "📋 Plan Generated"
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
	if act.ProgressUpdated.Title != "" {
		title := formatTelegramHTML(act.ProgressUpdated.Title)
		if act.ProgressUpdated.Description != "" {
			if len(act.ProgressUpdated.Description) > 120 {
				// Use expandable blockquote — plain text inside to avoid nesting issues
				return fmt.Sprintf("[%s] ✅ <b>%s</b>\n<blockquote expandable>%s</blockquote>", ts, title, escapeHTML(act.ProgressUpdated.Description))
			}
			desc := formatTelegramHTML(act.ProgressUpdated.Description)
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
	const maxLen = 4000 // Telegram limit is 4096, leave room for safety
	if len(msg) <= maxLen {
		return msg
	}

	// Find the header end
	headerEnd := strings.Index(msg, "\n\n")
	if headerEnd < 0 {
		headerEnd = 0
	} else {
		headerEnd += 2
	}

	header := msg[:headerEnd]
	body := msg[headerEnd:]

	// Trim oldest lines from body until it fits
	lines := strings.Split(body, "\n")
	for len(header)+len(strings.Join(lines, "\n")) > maxLen && len(lines) > 1 {
		lines = lines[1:]
	}

	return header + "...\n" + strings.Join(lines, "\n")
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
