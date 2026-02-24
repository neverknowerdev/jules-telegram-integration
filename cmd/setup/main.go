package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"jules-telegram-bot/internal/telegram"
)

func main() {
	fmt.Println("Welcome to Jules Telegram Bot Setup!")

	// 1. Check gcloud
	checkGcloud()

	// 2. Get Project ID
	projectID := getProjectID()
	fmt.Printf("Using Google Cloud Project: %s\n", projectID)

	// 3. Jules API Key
	apiKey := prompt("Enter your Jules API Key: ")

	// 4. Select Source - REMOVED, now supporting all sources.

	// 5. Telegram Token
	telegramToken := prompt("Enter your Telegram Bot Token: ")

	// 6. Polling Interval
	interval := prompt("Enter polling interval (cron format, e.g., '*/5 * * * *' for 5 mins): ")
	if interval == "" {
		interval = "*/5 * * * *"
	}

	// 7. Create Firestore
	fmt.Println("\nSetting up Firestore...")
	runCommand("gcloud", "firestore", "databases", "create", "--location=us-central1", "--type=firestore-native", "--quiet")

	// 8. Deploy Webhook Function
	fmt.Println("\nDeploying Webhook Function (this may take a few minutes)...")

	// Only env vars needed are API Key and Token. Source binding is dynamic.
	envVars := fmt.Sprintf("JULES_API_KEY=%s,TELEGRAM_TOKEN=%s,GCP_PROJECT=%s", apiKey, telegramToken, projectID)

	webhookCmd := []string{
		"functions", "deploy", "jules-telegram-webhook",
		"--gen2",
		"--region=us-central1",
		"--runtime=go121",
		"--source=.",
		"--entry-point=TelegramWebhook",
		"--trigger-http",
		"--allow-unauthenticated",
		"--set-env-vars=" + envVars,
		"--quiet",
	}
	if err := runCommand("gcloud", webhookCmd...); err != nil {
		log.Fatalf("Failed to deploy webhook: %v", err)
	}

	// Get Webhook URL
	webhookURL := getFunctionURL("jules-telegram-webhook")
	fmt.Printf("Webhook Deployed at: %s\n", webhookURL)

	// Set Telegram Webhook
	fmt.Println("Setting Telegram Webhook...")
	tgClient := telegram.NewClient(telegramToken)
	if err := tgClient.SetWebhook(webhookURL); err != nil {
		log.Fatalf("Failed to set Telegram webhook: %v", err)
	}

	// 9. Deploy Poller Function
	fmt.Println("\nDeploying Poller Function...")
	pollerCmd := []string{
		"functions", "deploy", "jules-poller",
		"--gen2",
		"--region=us-central1",
		"--runtime=go121",
		"--source=.",
		"--entry-point=JulesPoller",
		"--trigger-http",
		"--no-allow-unauthenticated",
		"--set-env-vars=" + envVars,
		"--quiet",
	}
	if err := runCommand("gcloud", pollerCmd...); err != nil {
		log.Fatalf("Failed to deploy poller: %v", err)
	}

	pollerURL := getFunctionURL("jules-poller")

	// Create Service Account for Scheduler
	saEmail := fmt.Sprintf("%s-compute@developer.gserviceaccount.com", getProjectNumber(projectID))

	// Grant Invoker role
	fmt.Println("Granting invoker role to service account...")
	runCommand("gcloud", "functions", "add-invoker-policy-binding", "jules-poller",
		"--region=us-central1",
		"--member=serviceAccount:"+saEmail,
		"--quiet",
	)

	// Create Scheduler Job
	fmt.Println("\nCreating Cloud Scheduler Job...")
	runCommand("gcloud", "scheduler", "jobs", "delete", "jules-poller-job", "--location=us-central1", "--quiet")

	schedCmd := []string{
		"scheduler", "jobs", "create", "http", "jules-poller-job",
		"--location=us-central1",
		"--schedule=" + interval,
		"--uri=" + pollerURL,
		"--oidc-service-account-email=" + saEmail,
		"--http-method=GET",
		"--quiet",
	}
	if err := runCommand("gcloud", schedCmd...); err != nil {
		log.Printf("Warning: Failed to create scheduler job: %v. You might need to enable Cloud Scheduler API.", err)
	}

	fmt.Println("\nSetup Complete! Start a chat with your bot and send /start.")
}

func checkGcloud() {
	if _, err := exec.LookPath("gcloud"); err != nil {
		log.Fatal("gcloud CLI is not installed. Please install it first.")
	}
}

func getProjectID() string {
	out, err := exec.Command("gcloud", "config", "get-value", "project").Output()
	if err != nil {
		log.Fatal("Failed to get gcloud project. Run 'gcloud init'.")
	}
	return strings.TrimSpace(string(out))
}

func getProjectNumber(projectID string) string {
	out, err := exec.Command("gcloud", "projects", "describe", projectID, "--format=value(projectNumber)").Output()
	if err != nil {
		log.Printf("Warning: Could not get project number.")
		return "PROJECT_NUMBER"
	}
	return strings.TrimSpace(string(out))
}

func getFunctionURL(funcName string) string {
	out, err := exec.Command("gcloud", "functions", "describe", funcName, "--gen2", "--region=us-central1", "--format=value(serviceConfig.uri)").Output()
	if err != nil {
		log.Fatal("Failed to get function URL.")
	}
	return strings.TrimSpace(string(out))
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func prompt(label string) string {
	fmt.Print(label)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}
