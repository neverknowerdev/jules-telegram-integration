package julestelegramintegration

import (
	"net/http"

	"github.com/neverknowerdev/jules-telegram-bot/functions/poller"
	"github.com/neverknowerdev/jules-telegram-bot/functions/webhook"

	"github.com/GoogleCloudPlatform/functions-framework-go/functions"
)

func init() {
	functions.HTTP("JulesPoller", JulesPoller)
	functions.HTTP("TelegramWebhook", TelegramWebhook)
}

func TelegramWebhook(w http.ResponseWriter, r *http.Request) {
	webhook.TelegramWebhook(w, r)
}

func JulesPoller(w http.ResponseWriter, r *http.Request) {
	poller.JulesPoller(w, r)
}
