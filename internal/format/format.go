package format

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/neverknowerdev/jules-telegram-bot/internal/jules"
)

func EscapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func TelegramHTML(md string) string {
	md = EscapeHTML(md)
	reBold := regexp.MustCompile(`(?s)\*\*(.*?)\*\*`)
	md = reBold.ReplaceAllString(md, "<b>$1</b>")
	reItalic := regexp.MustCompile(`(?s)\*(.*?)\*`)
	md = reItalic.ReplaceAllString(md, "<i>$1</i>")
	reCodeBlock := regexp.MustCompile("(?s)```(.*?)```")
	md = reCodeBlock.ReplaceAllString(md, "<pre>$1</pre>")
	reInline := regexp.MustCompile("(?s)`(.*?)`")
	md = reInline.ReplaceAllString(md, "<code>$1</code>")
	return md
}

func Timestamp(createTime string) string {
	if len(createTime) >= 16 {
		return createTime[11:16]
	}
	return "??:??"
}

func Plan(act jules.Activity) string {
	title := "📋 Plan Generated"
	if act.PlanGenerated == nil {
		return title
	}
	desc := TelegramHTML(act.PlanGenerated.Plan.Title)
	if desc != "" {
		desc += "\n\n"
	}
	for i, step := range act.PlanGenerated.Plan.Steps {
		stepTitle := TelegramHTML(step.Title)
		desc += fmt.Sprintf("<b>%d. %s</b>\n", i+1, stepTitle)
		if step.Description != "" {
			cleanDesc := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(step.Description), "-"))
			desc += fmt.Sprintf("<i>%s</i>\n", TelegramHTML(cleanDesc))
		}
		desc += "\n"
	}
	return fmt.Sprintf("%s\n%s", title, strings.TrimSpace(desc))
}

func AgentMessage(text string) string {
	if len(text) > 200 {
		preview := text
		if len(preview) > 100 {
			preview = preview[:100] + "..."
		}
		escapedFull := EscapeHTML(text)
		return fmt.Sprintf("💬 <b>Jules</b>\n%s\n<blockquote expandable>%s</blockquote>",
			TelegramHTML(preview), escapedFull)
	}
	return fmt.Sprintf("💬 <b>Jules</b>\n%s", TelegramHTML(text))
}

func ExecutionPlanLine(act jules.Activity, ts string) string {
	if act.PlanGenerated == nil {
		return ""
	}
	stepCount := len(act.PlanGenerated.Plan.Steps)
	planTitle := act.PlanGenerated.Plan.Title
	if planTitle == "" {
		planTitle = "Updated plan"
	}
	planTitle = EscapeHTML(planTitle)

	var steps strings.Builder
	for i, step := range act.PlanGenerated.Plan.Steps {
		steps.WriteString(fmt.Sprintf("%d. %s\n", i+1, EscapeHTML(step.Title)))
	}
	return fmt.Sprintf("[%s] 📋 <b>%s</b> (%d steps)\n<blockquote expandable>%s</blockquote>",
		ts, planTitle, stepCount, strings.TrimSpace(steps.String()))
}

func ProgressLine(act jules.Activity, ts string) string {
	if act.ProgressUpdated != nil && act.ProgressUpdated.Title != "" {
		title := TelegramHTML(act.ProgressUpdated.Title)
		if act.ProgressUpdated.Description != "" {
			descText := act.ProgressUpdated.Description
			if len(descText) > 400 {
				descText = descText[:400] + "..."
			}
			if len(descText) > 120 {
				return fmt.Sprintf("[%s] ✅ <b>%s</b>\n<blockquote expandable>%s</blockquote>", ts, title, EscapeHTML(descText))
			}
			return fmt.Sprintf("[%s] ✅ <b>%s</b>\n<i>%s</i>", ts, title, TelegramHTML(descText))
		}
		return fmt.Sprintf("[%s] ✅ <b>%s</b>", ts, title)
	}
	if len(act.Artifacts) > 0 {
		return fmt.Sprintf("[%s] 📝 Working...", ts)
	}
	return ""
}

func CompletionMessage(session *jules.Session) string {
	msg := "✅ <b>Jules has completed the task!</b>\n\n"
	for _, output := range session.Outputs {
		if output.PullRequest != nil {
			msg += fmt.Sprintf("🔀 <b>PR created:</b> <a href=\"%s\">%s</a>\n",
				output.PullRequest.URL, EscapeHTML(output.PullRequest.Title))
		}
		if output.ChangeSet != nil && output.ChangeSet.GitPatch.SuggestedCommitMessage != "" {
			commitMsg := output.ChangeSet.GitPatch.SuggestedCommitMessage
			if len(commitMsg) > 200 {
				msg += fmt.Sprintf("📝 <b>Commit message:</b>\n<blockquote expandable>%s</blockquote>",
					EscapeHTML(commitMsg))
			} else {
				msg += fmt.Sprintf("📝 <b>Commit message:</b>\n<i>%s</i>", EscapeHTML(commitMsg))
			}
		}
	}
	return msg
}

func ActivitySummary(act jules.Activity) string {
	var title, desc string
	if act.PlanGenerated != nil && len(act.PlanGenerated.Plan.Steps) > 0 {
		title = "📋 Plan Generated"
		desc = TelegramHTML(act.PlanGenerated.Plan.Title)
		if desc != "" {
			desc += "\n\n"
		}
		for i, step := range act.PlanGenerated.Plan.Steps {
			stepTitle := TelegramHTML(step.Title)
			desc += fmt.Sprintf("<b>%d. %s</b>\n", i+1, stepTitle)
			if step.Description != "" {
				cleanDesc := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(step.Description), "-"))
				desc += fmt.Sprintf("<i>%s</i>\n", TelegramHTML(cleanDesc))
			}
			desc += "\n"
		}
		desc = strings.TrimSpace(desc)
	} else if act.Originator == "user" {
		title = "You"
		if act.UserMessaged != nil && act.UserMessaged.UserMessage != "" {
			desc = TelegramHTML(act.UserMessaged.UserMessage)
		}
	} else if act.Originator == "agent" {
		title = "Jules"
		if act.AgentMessaged != nil && act.AgentMessaged.AgentMessage != "" {
			desc = TelegramHTML(act.AgentMessaged.AgentMessage)
		}
	}
	title = EscapeHTML(title)
	if desc != "" {
		return fmt.Sprintf("🤖 <b>%s</b>\n%s", title, desc)
	}
	return fmt.Sprintf("🤖 <b>%s</b>", title)
}
