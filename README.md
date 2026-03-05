# Jules Telegram Bot Integration

This repository contains the code and setup scripts to integrate Jules (Google's AI coding agent) with Telegram.

## Features

- **Receive Updates**: Get notified in Telegram for every new activity in your active Jules sessions.
- **Send Messages**: Reply directly in Telegram to send messages to Jules.
- **Task Management (Channels / Forum Topics)**:
    - Automatically maps each Jules task to a separate Forum Topic within a supergroup.
    - Create a new topic in Telegram to automatically start a new Jules task.
    - Archive a task in Telegram to automatically close and archive the Jules session and delete the topic.
- **Multi-User/Chat Support**: Supports multiple Telegram chats, each bound to a specific Jules source/repository.

## Prerequisites

1.  **Google Cloud Platform Account**: You need a GCP project with billing enabled (Free Tier is usually sufficient).
2.  **Jules API Key**: Generate one from the [Jules Web App Settings](https://jules.google.com/settings).
3.  **Telegram Bot Token**: Create a new bot with [@BotFather](https://t.me/BotFather) on Telegram. Note that your bot should be added as an Admin with permissions to manage topics if used in a forum.
4.  **Google Cloud CLI (gcloud)**: Installed and authenticated on your machine. [Installation Guide](https://cloud.google.com/sdk/docs/install).

## Setting up a Telegram Forum (Recommended UX)

For the best experience, we recommend using this bot in a Telegram Group with the "Topics" feature enabled. This allows each Jules task to be mapped to a separate conversation thread (similar to Slack channels).

1. **Create a Group**: Open Telegram and create a new group.
2. **Add Bot as Admin**: Add your newly created Telegram Bot to the group and promote it to Admin. Give it permissions to "Manage Topics" and "Delete Messages".
3. **Enable Topics**: Go to the group settings and enable "Topics" (this requires the group to have at least a few members, or just enable it directly in supergroups).
4. **Start the Bot**: Type `/start` in the "General" topic.
5. **Create a New Task**:
   - Simply create a new Topic in the group (e.g. name it "Fix login bug").
   - The bot will detect the new topic and prompt you to select a repository.
   - Reply to the bot in the topic with your first instruction for Jules. All updates for this task will stay contained within this topic!

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
