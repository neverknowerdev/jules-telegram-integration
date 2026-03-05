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

		minInteractiveInterval := 999999
		anyWaitingNow := false

		// Process all chats from Firestore using iterator to save memory
		err := firestoreClient.IterateAllChats(ctx, func(chat firestore.ChatConfig) error {
			if chat.CurrentSession == "" {
				return nil
			}

			if chat.IsWaitingForJules {
				anyWaitingNow = true
				if chat.InteractiveInterval > 0 && chat.InteractiveInterval < minInteractiveInterval {
					minInteractiveInterval = chat.InteractiveInterval
				}
			} else {
				// Throttle standard polling
				sinceLastPoll := time.Now().Unix() - chat.LastStandardPoll
				standardIntervalSecs := int64(chat.StandardInterval) * 60
				if chat.LastStandardPoll > 0 && sinceLastPoll < standardIntervalSecs {
					// Skip this chat until its standard interval has passed
					return nil
				}
			}

			if !chat.IsWaitingForJules {
				firestoreClient.UpdateLastStandardPoll(ctx, chat.ChatID, time.Now().Unix())
			}

			log.Printf("[POLLER] Processing chat %d", chat.ChatID)

			// Defer a recover within the loop to catch panics per chat
			defer func() {
				if r := recover(); r != nil {
					errPart := fmt.Sprintf("🚨 <b>Technical Error in Poller</b>\n\n<blockquote>%v</blockquote>", r)
					if telegramClient != nil {
						telegramClient.SendMessage(chat.ChatID, errPart)
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

			julesWorking := false
			if session.State == "IN_PROGRESS" || session.State == "PLANNING" || session.State == "AWAITING_PLAN_APPROVAL" {
				julesWorking = true
			}

			if chat.IsWaitingForJules && !julesWorking {
				// Jules finished. Reset flag to false immediately.
				firestoreClient.UpdateIsWaitingForJules(ctx, chat.ChatID, false)
				chat.IsWaitingForJules = false
			} else if !chat.IsWaitingForJules && julesWorking {
				// We detected an active task during standard polling. Switch to interactive mode.
				firestoreClient.UpdateIsWaitingForJules(ctx, chat.ChatID, true)
				chat.IsWaitingForJules = true
				anyWaitingNow = true // Mark waiting
				if chat.InteractiveInterval > 0 && chat.InteractiveInterval < minInteractiveInterval {
					minInteractiveInterval = chat.InteractiveInterval
				}
			}

			// 2. Detect session state transitions (e.g. COMPLETED -> IN_PROGRESS)
			isTransitioningToActive := (chat.State == "COMPLETED" || chat.State == "FAILED") && julesWorking

			if isTransitioningToActive {
				log.Printf("[POLLER] Chat %d: session re-activated (%s -> %s), resetting tracking", chat.ChatID, chat.State, session.State)
				firestoreClient.UpdateProgressMessageID(ctx, chat.ChatID, 0)
				chat.ProgressMessageID = 0
			}

			// Update tracked state in Firestore if changed
			if chat.State != session.State {
				firestoreClient.UpdateChatState(ctx, chat.ChatID, session.State, chat.DraftSource)
				chat.State = session.State
			}

			activities, err := julesClient.ListActivities(chat.CurrentSession, chat.LastActivityID)
			if err != nil {
				log.Printf("Failed to list activities for chat %d: %v", chat.ChatID, err)
				return nil
			}

			if len(activities) == 0 {
				return nil
			}

			log.Printf("[POLLER] Chat %d: fetched %d new activities from Jules", chat.ChatID, len(activities))

			newestID := activities[len(activities)-1].Id

			// First run for this session: just set cursor to newest and skip sending.
			if chat.LastActivityID == "" {
				log.Printf("[POLLER] Chat %d: first run, marking newest activity %q and skipping", chat.ChatID, newestID)
				firestoreClient.UpdateLastActivity(ctx, chat.ChatID, newestID)
				return nil
			}

			newActivities := activities

			roundStartIdx := -1
			for i := len(activities) - 1; i >= 0; i-- {
				if activities[i].SessionCompleted != nil || activities[i].SessionFailed != nil {
					roundStartIdx = i
					break
				}
			}

			isWorkStarted := func(planIdx int) bool {
				for i := roundStartIdx + 1; i < planIdx; i++ {
					if activities[i].ProgressUpdated != nil && activities[i].ProgressUpdated.Title != "" {
						return true
					}
					if len(activities[i].Artifacts) > 0 {
						return true
					}
				}
				return false
			}

			executionPlans := make(map[string]bool)
			for i, act := range activities {
				if act.PlanGenerated != nil && len(act.PlanGenerated.Plan.Steps) > 0 && isWorkStarted(i) {
					executionPlans[act.Id] = true
				}
			}

			hasNewProgress := isTransitioningToActive || (chat.ProgressMessageID == 0 && (session.State == "IN_PROGRESS" || session.State == "PLANNING"))
			progressMsgID := chat.ProgressMessageID

			if len(newActivities) > 0 {
				for _, act := range newActivities {
					if act.Originator == "user" {
						continue
					}

					if act.PlanGenerated != nil && len(act.PlanGenerated.Plan.Steps) > 0 {
						if executionPlans[act.Id] {
							hasNewProgress = true
						} else {
							log.Printf("[POLLER] Chat %d: sending approval plan", chat.ChatID)
							progressMsgID = 0
							firestoreClient.UpdateProgressMessageID(ctx, chat.ChatID, 0)
							msg := formatPlan(act)
							sessionIDShort := strings.TrimPrefix(chat.CurrentSession, "sessions/")
							keyboard := telegram.InlineKeyboardMarkup{
								InlineKeyboard: [][]telegram.InlineKeyboardButton{
									{{Text: "✅ Approve Plan", CallbackData: "approve_plan:" + sessionIDShort}},
								},
							}
							telegramClient.SendMessageWithKeyboard(chat.ChatID, msg, keyboard)
						}
						continue
					}

					if act.AgentMessaged != nil && act.AgentMessaged.AgentMessage != "" {
						msg := formatAgentMessage(act.AgentMessaged.AgentMessage)
						telegramClient.SendMessage(chat.ChatID, msg)
						continue
					}

					if act.SessionFailed != nil {
						reason := act.SessionFailed.Reason
						var errMsg string
						if reason != "" {
							errMsg = fmt.Sprintf("⚠️ <b>Jules encountered an error</b>\n\n<blockquote>%s</blockquote>", escapeHTML(reason))
						} else {
							errMsg = "⚠️ <b>Jules encountered an error</b>\n\nThe session failed unexpectedly."
						}
						telegramClient.SendMessage(chat.ChatID, errMsg)
						continue
					}

					if act.SessionCompleted != nil {
						msg := formatCompletionMessage(session)
						sessionIDShort := strings.TrimPrefix(chat.CurrentSession, "sessions/")
						keyboard := telegram.InlineKeyboardMarkup{
							InlineKeyboard: [][]telegram.InlineKeyboardButton{
								{
									{Text: "🔀 Create PR", CallbackData: "create_pr:" + sessionIDShort},
									{Text: "🌿 Create Branch", CallbackData: "create_branch:" + sessionIDShort},
								},
								{{Text: "🔗 Open in Jules", URL: session.URL}},
							},
						}
						telegramClient.SendMessageWithKeyboard(chat.ChatID, msg, keyboard)
						continue
					}

					if (act.ProgressUpdated != nil && act.ProgressUpdated.Title != "") || len(act.Artifacts) > 0 {
						hasNewProgress = true
					}
				}
				firestoreClient.UpdateLastActivity(ctx, chat.ChatID, newestID)
			}

			if hasNewProgress {
				lastApprovedPlanIdx := -1
				for i := len(activities) - 1; i > roundStartIdx; i-- {
					if activities[i].PlanGenerated != nil && len(activities[i].PlanGenerated.Plan.Steps) > 0 && !executionPlans[activities[i].Id] {
						lastApprovedPlanIdx = i
						break
					}
				}

				startIdx := roundStartIdx
				if lastApprovedPlanIdx > roundStartIdx {
					startIdx = lastApprovedPlanIdx
				}

				var allProgressLines []string
				for i := startIdx + 1; i < len(activities); i++ {
					act := activities[i]
					if act.Originator == "user" {
						continue
					}
					ts := formatTimestamp(act.CreateTime)
					if act.PlanGenerated != nil && len(act.PlanGenerated.Plan.Steps) > 0 && executionPlans[act.Id] {
						if line := formatExecutionPlanLine(act, ts); line != "" {
							allProgressLines = append(allProgressLines, line)
						}
					} else if line := formatProgressLine(act, ts); line != "" {
						allProgressLines = append(allProgressLines, line)
					}
				}

				if len(allProgressLines) > 15 {
					allProgressLines = allProgressLines[len(allProgressLines)-15:]
				}

				if len(allProgressLines) > 0 || progressMsgID == 0 {
					header := "⚙️ <b>Jules is working on it...</b>\n\n"
					body := strings.Join(allProgressLines, "\n\n")
					if body == "" {
						body = "<i>Jules is thinking...</i>"
					}
					msg := header + body

					if progressMsgID == 0 {
						if msgID, err := telegramClient.SendMessageReturningID(chat.ChatID, msg); err == nil {
							firestoreClient.UpdateProgressMessageID(ctx, chat.ChatID, msgID)
							chat.ProgressMessageID = msgID
						} else {
							log.Printf("[POLLER] Chat %d: FAILED to create progress message: %v", chat.ChatID, err)
						}
					} else {
						if err := telegramClient.EditMessageText(chat.ChatID, progressMsgID, msg, nil); err != nil {
							log.Printf("[POLLER] Chat %d: edit failed: %v", chat.ChatID, err)
							if msgID, err := telegramClient.SendMessageReturningID(chat.ChatID, msg); err == nil {
								firestoreClient.UpdateProgressMessageID(ctx, chat.ChatID, msgID)
								chat.ProgressMessageID = msgID
							} else {
								log.Printf("[POLLER] Chat %d: FAILED to create replacement progress message: %v", chat.ChatID, err)
							}
						}
					}
				}
			}

			for _, output := range session.Outputs {
				if output.PullRequest != nil {
					prURL := output.PullRequest.URL
					if !chat.NotifiedPRs[prURL] {
						msg := fmt.Sprintf("🔀 <b>New Pull Request Created!</b>\n\n<b>Title:</b> %s\n<b>Branch:</b> <code>%s</code> → <code>%s</code>",
							escapeHTML(output.PullRequest.Title), escapeHTML(output.PullRequest.HeadRef), escapeHTML(output.PullRequest.BaseRef))
						keyboard := telegram.InlineKeyboardMarkup{
							InlineKeyboard: [][]telegram.InlineKeyboardButton{{{Text: "🔗 View Pull Request", URL: prURL}}},
						}
						if err := telegramClient.SendMessageWithKeyboard(chat.ChatID, msg, keyboard); err == nil {
							firestoreClient.MarkPRAsNotified(ctx, chat.ChatID, prURL)
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
							if err := telegramClient.SendMessage(chat.ChatID, msg); err == nil {
								firestoreClient.MarkBranchAsNotified(ctx, chat.ChatID, branchName)
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

		if !anyWaitingNow {
			log.Println("No tasks waiting for Jules. Exiting long-running poller.")
			break
		}

		time.Sleep(time.Duration(minInteractiveInterval) * time.Second)
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
