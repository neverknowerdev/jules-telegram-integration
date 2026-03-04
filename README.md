# Jules Telegram Bot Integration

This repository contains the code and setup scripts to integrate Jules (Google's AI coding agent) with Telegram.

## Features

- **Receive Updates**: Get notified in Telegram for every new activity in your active Jules sessions.
- **Send Messages**: Reply directly in Telegram to send messages to Jules.
- **Task Management**:
    - `/tasks`: List all active Jules sessions.
    - `/status`: View the status of the currently bound session.
    - `/switch <session_id>`: Switch the active session for the current chat.
- **Multi-User/Chat Support**: Supports multiple Telegram chats, each bound to a specific Jules source/repository.

## Prerequisites

1.  **Google Cloud Platform Account**: You need a GCP project with billing enabled (Free Tier is usually sufficient).
2.  **Jules API Key**: Generate one from the [Jules Web App Settings](https://jules.google.com/settings).
3.  **Telegram Bot Token**: Create a new bot with [@BotFather](https://t.me/BotFather) on Telegram.
4.  **Google Cloud CLI (gcloud)**: Installed and authenticated on your machine. [Installation Guide](https://cloud.google.com/sdk/docs/install).

## Installation

### Automated Setup (Recommended)

We provide a bash setup script that automates the entire deployment process.

1.  **Clone the repository**:
    ```bash
    git clone https://github.com/your-repo/jules-telegram-bot.git
    cd jules-telegram-bot
    ```
2.  **Run the setup script**:
    ```bash
    ./scripts/install.sh
    ```
3.  **Follow the interactive prompts**:
    - The tool will check your `gcloud` authentication.
    - It will ask for your Jules API Key and Telegram Bot Token.
    - It will help you select a Jules Source (Repository) to bind.
    - It will deploy the necessary Cloud Functions and Cloud Scheduler jobs to your GCP project.

### Manual Deploy

If you prefer to deploy manually without running the setup script:

1.  **Deploy Cloud Functions**:
    (Refer to the `scripts/install.sh` logic for the exact `gcloud functions deploy` commands used).

## Architecture

- **Webhook Function**: A Cloud Function that receives updates from Telegram and handles user commands.
- **Poller Function**: A Cloud Function triggered by Cloud Scheduler (default every 5 mins) to check for new Jules activities.
- **Firestore**: Used to store the mapping between Telegram Chat IDs and Jules Sessions/Sources, and to track the last seen activity.

## Development

### Running Tests

```bash
go test ./...
```
