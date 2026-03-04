#!/bin/bash
set -e

echo "Welcome to Jules Telegram Bot Setup!"

# Load environment variables from .env or local.env if they exist
for env_file in ".env" "local.env"; do
    if [ -f "$env_file" ]; then
        echo "Loading environment variables from $env_file file..."
        set -o allexport
        source "$env_file"
        set +o allexport
    fi
done

# 1. Check dependencies
if ! command -v gcloud &> /dev/null; then
    echo "gcloud CLI is not installed. Please install it (https://cloud.google.com/sdk/docs/install)."
    exit 1
fi

if ! command -v jq &> /dev/null; then
    echo "jq is required but not installed. Please install it (e.g., brew install jq or apt install jq)."
    exit 1
fi

if ! command -v curl &> /dev/null; then
    echo "curl is required but not installed. Please install it."
    exit 1
fi

# 2. Get Project ID
PROJECT_ID=$(gcloud config get-value project 2>/dev/null)
if [ -z "$PROJECT_ID" ]; then
    echo "Failed to get gcloud project. Run 'gcloud init'."
    exit 1
fi
echo "Using Google Cloud Project: $PROJECT_ID"

# 3. Jules API Key
if [ -z "$JULES_API_KEY" ]; then
    read -p "Enter your Jules API Key: " API_KEY
else
    API_KEY="$JULES_API_KEY"
    echo "Using Jules API Key from environment."
fi

# 4. Select Source
echo "Fetching repositories from Jules..."
JULES_BASE_URL="https://jules.googleapis.com/v1alpha"
SOURCES_JSON=$(curl -s -w "\n%{http_code}" -H "X-Goog-Api-Key: $API_KEY" "$JULES_BASE_URL/sources")

HTTP_CODE=$(echo "$SOURCES_JSON" | tail -n1)
SOURCES_BODY=$(echo "$SOURCES_JSON" | sed '$ d')

if [ "$HTTP_CODE" != "200" ]; then
    echo "Failed to list sources. Check your API Key. (HTTP $HTTP_CODE)"
    echo "$SOURCES_BODY"
    exit 1
fi

SOURCE_NAMES=()
while IFS= read -r line; do
    [ -n "$line" ] && SOURCE_NAMES+=("$line")
done < <(echo "$SOURCES_BODY" | jq -r ".sources[]? | .name")

SOURCE_DISPLAYS=()
while IFS= read -r line; do
    [ -n "$line" ] && SOURCE_DISPLAYS+=("$line")
done < <(echo "$SOURCES_BODY" | jq -r ".sources[]? | .displayName")

if [ ${#SOURCE_NAMES[@]} -eq 0 ]; then
    echo "No sources found. Please connect a repository in Jules Web App first."
    exit 1
fi

echo ""
echo "Available Repositories:"
for i in "${!SOURCE_NAMES[@]}"; do
    echo "- ${SOURCE_DISPLAYS[$i]} (${SOURCE_NAMES[$i]})"
done

# Join all sources with a comma
SELECTED_SOURCES=$(IFS=,; echo "${SOURCE_NAMES[*]}")
echo "Using Sources: $SELECTED_SOURCES"

# 5. Telegram Token
if [ -z "$TELEGRAM_TOKEN" ]; then
    read -p "Enter your Telegram Bot Token: " TELEGRAM_TOKEN
else
    echo "Using Telegram Bot Token from environment."
fi

# 6. Polling Interval
if [ -z "$POLLING_INTERVAL_MINUTES" ]; then
    while true; do
        read -p "Enter polling interval in minutes (e.g., 5 for every 5 mins) [5]: " INTERVAL_MINUTES
        if [ -z "$INTERVAL_MINUTES" ]; then
            INTERVAL="*/5 * * * *"
            break
        elif [[ "$INTERVAL_MINUTES" =~ ^[0-9]+$ ]] && [ "$INTERVAL_MINUTES" -ge 1 ] && [ "$INTERVAL_MINUTES" -le 59 ]; then
            INTERVAL="*/$INTERVAL_MINUTES * * * *"
            break
        else
            echo "Please enter a valid number between 1 and 59."
        fi
    done
else
    if [[ "$POLLING_INTERVAL_MINUTES" =~ ^[0-9]+$ ]] && [ "$POLLING_INTERVAL_MINUTES" -ge 1 ] && [ "$POLLING_INTERVAL_MINUTES" -le 59 ]; then
        INTERVAL="*/$POLLING_INTERVAL_MINUTES * * * *"
        echo "Using Polling Interval from environment ($POLLING_INTERVAL_MINUTES mins)."
    else
        echo "Invalid POLLING_INTERVAL_MINUTES env var. Must be a number between 1 and 59."
        exit 1
    fi
fi

# 7. Create Firestore
echo ""
echo "Setting up Firestore..."
gcloud firestore databases create --location=us-central1 --type=firestore-native --quiet || true

# 8. Deploy Webhook Function
echo ""
echo "Deploying Webhook Function (this may take a few minutes)..."

ENV_VARS="^@^JULES_API_KEY=$API_KEY@TELEGRAM_TOKEN=$TELEGRAM_TOKEN@SELECTED_SOURCES=$SELECTED_SOURCES@GCP_PROJECT=$PROJECT_ID"

gcloud functions deploy jules-telegram-webhook \
    --gen2 \
    --region=us-central1 \
    --runtime=go121 \
    --source=. \
    --entry-point=TelegramWebhook \
    --trigger-http \
    --allow-unauthenticated \
    --set-env-vars="$ENV_VARS" \
    --quiet

# Get Webhook URL
WEBHOOK_URL=$(gcloud functions describe jules-telegram-webhook --gen2 --region=us-central1 --format="value(serviceConfig.uri)")
echo "Webhook Deployed at: $WEBHOOK_URL"

# Set Telegram Webhook
echo "Setting Telegram Webhook..."
curl -s -X POST "https://api.telegram.org/bot$TELEGRAM_TOKEN/setWebhook" -d "url=$WEBHOOK_URL" > /dev/null

# 9. Deploy Poller Function
echo ""
echo "Deploying Poller Function..."
gcloud functions deploy jules-poller \
    --gen2 \
    --region=us-central1 \
    --runtime=go121 \
    --source=. \
    --entry-point=JulesPoller \
    --trigger-http \
    --no-allow-unauthenticated \
    --set-env-vars="$ENV_VARS" \
    --quiet

POLLER_URL=$(gcloud functions describe jules-poller --gen2 --region=us-central1 --format="value(serviceConfig.uri)")

PROJECT_NUMBER=$(gcloud projects describe "$PROJECT_ID" --format="value(projectNumber)")
if [ -z "$PROJECT_NUMBER" ]; then
    echo "Warning: Could not get project number. Service Account email might be wrong."
    PROJECT_NUMBER="PROJECT_NUMBER"
fi

SA_EMAIL="${PROJECT_NUMBER}-compute@developer.gserviceaccount.com"

echo "Granting invoker role to service account ($SA_EMAIL)..."
gcloud functions add-invoker-policy-binding jules-poller \
    --region=us-central1 \
    --member="serviceAccount:$SA_EMAIL" \
    --quiet

# 10. Create Scheduler Job
echo ""
echo "Creating Cloud Scheduler Job..."
gcloud scheduler jobs delete jules-poller-job --location=us-central1 --quiet || true

gcloud scheduler jobs create http jules-poller-job \
    --location=us-central1 \
    --schedule="$INTERVAL" \
    --uri="$POLLER_URL" \
    --oidc-service-account-email="$SA_EMAIL" \
    --http-method=GET \
    --quiet || echo "Warning: Failed to create scheduler job. You might need to enable Cloud Scheduler API."

echo ""
echo "Setup Complete! Start a chat with your bot and send /start."
