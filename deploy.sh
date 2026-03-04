#!/bin/bash

# Exit on error
set -e

source local.env

echo "Deploying Webhook Function to GCP..."

gcloud functions deploy jules-telegram-webhook \
    --gen2 \
    --region=us-central1 \
    --runtime=go121 \
    --source=. \
    --entry-point=TelegramWebhook \
    --trigger-http \
    --allow-unauthenticated \
    --quiet

echo "Webhook deployment complete!"

echo "Deploying Poller Function to GCP..."

gcloud functions deploy jules-poller \
    --gen2 \
    --region=us-central1 \
    --runtime=go121 \
    --source=. \
    --entry-point=JulesPoller \
    --trigger-http \
    --no-allow-unauthenticated \
    --quiet

echo "Poller deployment complete!"
