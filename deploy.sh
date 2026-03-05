#!/bin/bash

# Exit on error
set -e

source local.env


echo "Deploying Poller Function to GCP..."

gcloud functions deploy jules-poller \
    --gen2 \
    --region=us-central1 \
    --runtime=go123 \
    --source=. \
    --entry-point=JulesPoller \
    --trigger-http \
    --no-allow-unauthenticated \
    --timeout=3600 \
    --quiet

echo "Poller deployment complete!"

echo "Configuring Poller to Never Sleep (Always Allocated CPU)..."
gcloud run services update jules-poller \
    --region=us-central1 \
    --no-cpu-throttling \
    --quiet

POLLER_URL=$(gcloud functions describe jules-poller --gen2 --region=us-central1 --format="value(serviceConfig.uri)")

echo "Deploying Webhook Function to GCP..."

gcloud functions deploy jules-telegram-webhook \
    --gen2 \
    --region=us-central1 \
    --runtime=go123 \
    --source=. \
    --entry-point=TelegramWebhook \
    --trigger-http \
    --allow-unauthenticated \
    --update-env-vars="POLLER_URL=${POLLER_URL}" \
    --quiet

echo "Webhook deployment complete!"
