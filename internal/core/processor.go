package core

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/neverknowerdev/jules-telegram-bot/internal/firestore"
	"github.com/neverknowerdev/jules-telegram-bot/internal/format"
	"github.com/neverknowerdev/jules-telegram-bot/internal/jules"
	"github.com/neverknowerdev/jules-telegram-bot/internal/telegram"
	"github.com/neverknowerdev/jules-telegram-bot/internal/telegraph"
)

type Processor struct {
	julesClient     jules.ClientInterface
	firestoreClient firestore.ClientInterface
	telegramClient  telegram.ClientInterface
	telegraphClient *telegraph.Client
}

func NewProcessor(jc jules.ClientInterface, fc firestore.ClientInterface, tc telegram.ClientInterface, telC *telegraph.Client) *Processor {
	return &Processor{
		julesClient:     jc,
		firestoreClient: fc,
		telegramClient:  tc,
		telegraphClient: telC,
	}
}

func (p *Processor) ProcessChat(ctx context.Context, chat *firestore.ChatConfig) error {
	if chat.CurrentSession == "" {
		return nil
	}
	log.Printf("[CORE] Processing chat %d (Thread %d)", chat.ChatID, chat.ThreadID)
	session, err := p.julesClient.GetSession(chat.CurrentSession)
	if err != nil {
		log.Printf("[CORE] Chat %d: failed to get session: %v", chat.ChatID, err)
		return err
	}
	isTransitioningToActive := (chat.State == "COMPLETED" || chat.State == "FAILED") &&
		(session.State == "IN_PROGRESS" || session.State == "PLANNING" || session.State == "AWAITING_PLAN_APPROVAL")
	if isTransitioningToActive {
		p.firestoreClient.UpdateProgressMessageID(ctx, chat.ChatID, chat.ThreadID, 0)
		chat.ProgressMessageID = 0
	}
	if chat.State != session.State {
		p.firestoreClient.UpdateChatState(ctx, chat.ChatID, chat.ThreadID, session.State, chat.DraftSource)
		chat.State = session.State
	}
	activities, err := p.julesClient.ListActivities(chat.CurrentSession, chat.LastActivityID)
	if err != nil {
		return err
	}
	if len(activities) == 0 {
		return p.processOutputs(ctx, chat, session)
	}
	newestID := activities[len(activities)-1].Id
	if chat.LastActivityID == "" {
		p.firestoreClient.UpdateLastActivity(ctx, chat.ChatID, chat.ThreadID, newestID)
		return p.processOutputs(ctx, chat, session)
	}
	var hasNewProgress bool = isTransitioningToActive || (chat.ProgressMessageID == 0 && (session.State == "IN_PROGRESS" || session.State == "PLANNING"))
	for i, act := range activities {
		if act.Originator == "user" {
			continue
		}
		if p.processSingleActivity(ctx, chat, session, activities, i) {
			hasNewProgress = true
		}
	}
	p.firestoreClient.UpdateLastActivity(ctx, chat.ChatID, chat.ThreadID, newestID)
	if hasNewProgress {
		p.updateProgressMessage(ctx, chat, session, activities)
	}
	return p.processOutputs(ctx, chat, session)
}

func (p *Processor) processSingleActivity(ctx context.Context, chat *firestore.ChatConfig, session *jules.Session, allActivities []jules.Activity, currentIndex int) bool {
	act := allActivities[currentIndex]
	if act.PlanGenerated != nil && len(act.PlanGenerated.Plan.Steps) > 0 {
		isLatestPlan := true
		for j := currentIndex + 1; j < len(allActivities); j++ {
			if allActivities[j].PlanGenerated != nil {
				isLatestPlan = false
				break
			}
		}
		if isLatestPlan && session.State == "AWAITING_PLAN_APPROVAL" {
			p.firestoreClient.UpdateProgressMessageID(ctx, chat.ChatID, chat.ThreadID, 0)
			msg := format.Plan(act)
			sessionIDShort := strings.TrimPrefix(chat.CurrentSession, "sessions/")
			keyboard := telegram.InlineKeyboardMarkup{
				InlineKeyboard: [][]telegram.InlineKeyboardButton{
					{{Text: "✅ Approve Plan", CallbackData: "approve_plan:" + sessionIDShort}},
				},
			}
			p.telegramClient.SendMessageWithKeyboard(chat.ChatID, chat.ThreadID, msg, keyboard)
			return false
		}
		return true
	}
	if act.AgentMessaged != nil && act.AgentMessaged.AgentMessage != "" {
		msg := format.AgentMessage(act.AgentMessaged.AgentMessage)
		p.telegramClient.SendMessage(chat.ChatID, chat.ThreadID, msg)
		return false
	}
	if act.SessionFailed != nil {
		reason := act.SessionFailed.Reason
		var errMsg string
		if reason != "" {
			errMsg = fmt.Sprintf("⚠️ <b>Jules encountered an error</b>\n\n<blockquote>%s</blockquote>", format.EscapeHTML(reason))
		} else {
			errMsg = "⚠️ <b>Jules encountered an error</b>\n\nThe session failed unexpectedly."
		}
		p.telegramClient.SendMessage(chat.ChatID, chat.ThreadID, errMsg)
		return false
	}
	if act.SessionCompleted != nil {
		msg := format.CompletionMessage(session)
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
		})
		keyboard := telegram.InlineKeyboardMarkup{InlineKeyboard: inlineButtons}
		p.telegramClient.SendMessageWithKeyboard(chat.ChatID, chat.ThreadID, msg, keyboard)
		return false
	}
	if (act.ProgressUpdated != nil && act.ProgressUpdated.Title != "") || len(act.Artifacts) > 0 {
		return true
	}
	return false
}

func (p *Processor) updateProgressMessage(ctx context.Context, chat *firestore.ChatConfig, session *jules.Session, activities []jules.Activity) {
	var allProgressLines []string
	for _, act := range activities {
		if act.Originator == "user" {
			continue
		}
		ts := format.Timestamp(act.CreateTime)
		isExecutionPlan := false
		if act.PlanGenerated != nil && len(act.PlanGenerated.Plan.Steps) > 0 {
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
			if line := format.ExecutionPlanLine(act, ts); line != "" {
				allProgressLines = append(allProgressLines, line)
			}
		} else if act.PlanGenerated == nil {
			if line := format.ProgressLine(act, ts); line != "" {
				if strings.HasSuffix(line, "📝 Working...") && len(allProgressLines) > 0 && strings.HasSuffix(allProgressLines[len(allProgressLines)-1], "📝 Working...") {
					allProgressLines[len(allProgressLines)-1] = line
				} else {
					allProgressLines = append(allProgressLines, line)
				}
			}
		}
	}
	if len(allProgressLines) == 0 && chat.ProgressMessageID != 0 {
		return
	}
	header := "⚙️ <b>Jules is working on it...</b>\n\n"
	var telegraphURL string
	if p.telegraphClient != nil && p.telegraphClient.AccessToken != "" {
		pageContent := buildTelegraphContent(allProgressLines)
		pageTitle := "Jules Task Logs"
		var currentTelegraphPath string
		if len(chat.TelegraphPages) > 0 {
			currentTelegraphPath = chat.TelegraphPages[len(chat.TelegraphPages)-1]
		}
		if currentTelegraphPath == "" {
			pageResp, err := p.telegraphClient.CreatePage(pageTitle, pageContent)
			if err == nil {
				telegraphURL = pageResp.Result.URL
				chat.TelegraphPages = append(chat.TelegraphPages, pageResp.Result.Path)
				p.firestoreClient.AppendTelegraphPage(ctx, chat.ChatID, chat.ThreadID, pageResp.Result.Path)
			}
		} else {
			pageResp, err := p.telegraphClient.EditPage(currentTelegraphPath, pageTitle, pageContent)
			if err == nil {
				telegraphURL = pageResp.Result.URL
			}
		}
	}
	var finalLines []string
	currentLen := len(header)
	omittedCount := 0
	for i := len(allProgressLines) - 1; i >= 0; i-- {
		lineLen := len(allProgressLines[i]) + 2
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
	if chat.ProgressMessageID == 0 {
		var msgID int
		var err error
		if keyboard != nil {
			msgID, err = p.telegramClient.SendMessageWithKeyboardReturningID(chat.ChatID, chat.ThreadID, msg, *keyboard)
		} else {
			msgID, err = p.telegramClient.SendMessageReturningID(chat.ChatID, chat.ThreadID, msg)
		}
		if err == nil {
			p.firestoreClient.UpdateProgressMessageID(ctx, chat.ChatID, chat.ThreadID, msgID)
			chat.ProgressMessageID = msgID
		}
	} else {
		editErr := p.telegramClient.EditMessageText(chat.ChatID, chat.ProgressMessageID, msg, keyboard)
		if editErr != nil {
			var msgID int
			var err error
			if keyboard != nil {
				msgID, err = p.telegramClient.SendMessageWithKeyboardReturningID(chat.ChatID, chat.ThreadID, msg, *keyboard)
			} else {
				msgID, err = p.telegramClient.SendMessageReturningID(chat.ChatID, chat.ThreadID, msg)
			}
			if err == nil {
				p.firestoreClient.UpdateProgressMessageID(ctx, chat.ChatID, chat.ThreadID, msgID)
				chat.ProgressMessageID = msgID
			}
		}
	}
}

func (p *Processor) processOutputs(ctx context.Context, chat *firestore.ChatConfig, session *jules.Session) error {
	for _, output := range session.Outputs {
		if output.PullRequest != nil {
			prURL := output.PullRequest.URL
			if !chat.NotifiedPRs[prURL] {
				msg := fmt.Sprintf("🔀 <b>New Pull Request Created!</b>\n\n<b>Title:</b> %s\n<b>Branch:</b> <code>%s</code> → <code>%s</code>",
					format.EscapeHTML(output.PullRequest.Title), format.EscapeHTML(output.PullRequest.HeadRef), format.EscapeHTML(output.PullRequest.BaseRef))
				keyboard := telegram.InlineKeyboardMarkup{
					InlineKeyboard: [][]telegram.InlineKeyboardButton{{{Text: "🔗 View Pull Request", URL: prURL}}},
				}
				p.telegramClient.UnpinAllChatMessages(chat.ChatID, chat.ThreadID)
				if msgID, err := p.telegramClient.SendMessageWithKeyboardReturningID(chat.ChatID, chat.ThreadID, msg, keyboard); err == nil {
					p.telegramClient.PinChatMessage(chat.ChatID, chat.ThreadID, msgID)
					p.firestoreClient.MarkPRAsNotified(ctx, chat.ChatID, chat.ThreadID, prURL)
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
					msg := fmt.Sprintf("🌿 <b>New GitHub Branch Created!</b>\n\n<b>Branch:</b> <code>%s</code>", format.EscapeHTML(branchName))
					if err := p.telegramClient.SendMessage(chat.ChatID, chat.ThreadID, msg); err == nil {
						p.firestoreClient.MarkBranchAsNotified(ctx, chat.ChatID, chat.ThreadID, branchName)
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
}

func buildTelegraphContent(lines []string) []telegraph.Node {
	var nodes []telegraph.Node
	for _, line := range lines {
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
