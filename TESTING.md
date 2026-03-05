# Testing Guide

This document describes the testing architecture, available tests, and common test scenarios for the Jules Telegram Bot.

## Architecture & Refactoring

To enable comprehensive testing, the codebase was refactored to use **Dependency Injection**.
Instead of relying on hardcoded HTTP clients or direct Firestore interactions within the Cloud Functions, the business logic now depends on interfaces:

- `jules.ClientInterface`
- `telegram.ClientInterface`
- `firestore.ClientInterface`

This decoupling allows us to replace real network calls with mock objects during tests.

## Mocks (`internal/mocks`)

The `internal/mocks` package provides robust, in-memory implementations of these interfaces:

- **`MockJulesClient`**: Simulates the Jules API. You can pre-configure it with mock `Sources`, `Sessions`, and `Activities`. It also records sent messages.
- **`MockTelegramClient`**: Simulates the Telegram API. It records all sent messages, keyboards, edited messages, answered callbacks, and created/deleted topics. This allows tests to assert exactly what the bot replied with.
- **`MockFirestoreClient`**: An in-memory key-value store simulating Firestore. It tracks `ChatConfig` states and records which update methods were called.

## Test Scenarios

### 1. Webhook Tests (`functions/webhook/webhook_test.go`)

These tests verify how the bot handles incoming updates from Telegram.

*   **`TestWebhook_StartCommand`**:
    *   **Scenario**: User sends `/start`.
    *   **Verification**: Ensures the chat config is saved to Firestore and a welcome message with a reply keyboard is sent back.
*   **`TestWebhook_TopicCreated`**:
    *   **Scenario**: A new Forum Topic is created in a Telegram supergroup.
    *   **Verification**: Ensures the bot detects the new topic, changes state to `waiting_for_repo`, and sends a repository selection message with inline buttons.
*   **`TestWebhook_NewTaskCommand`**:
    *   **Scenario**: User clicks the "➕ New Task" button.
    *   **Verification**: Ensures the bot fetches available sources from Jules and prompts the user to select one.
*   **`TestWebhook_HandleMessage_CreateSession`**:
    *   **Scenario**: User is in the `waiting_for_message` state and sends their first prompt.
    *   **Verification**: Ensures the bot successfully calls `CreateSession` on the Jules API, clears the waiting state, and updates the current session ID in Firestore.
*   **`TestWebhook_Callback_TopicRepo`**:
    *   **Scenario**: User clicks an inline button to select a repository for a new topic (e.g., `topicrepo:456:repo1`).
    *   **Verification**: Ensures the callback query is answered (to remove the loading spinner), the draft source is saved to Firestore, the state updates to `waiting_for_message`, and the message text is edited to prompt for user input.

### 2. Poller Tests (`functions/poller/poller_test.go`)

These tests verify the background job that checks Jules for updates.

*   **`TestPoller_EmptyChats`**:
    *   **Scenario**: Poller runs, but there are no active chats in Firestore.
    *   **Verification**: Ensures the poller gracefully exits without sending any messages.
*   **`TestPoller_NewActivities`**:
    *   **Scenario**: Poller runs for an active chat and finds new activities on the Jules session (e.g., a progress update and a direct agent message).
    *   **Verification**: Ensures direct agent messages are sent to Telegram immediately, the ongoing "Jules is working on it..." progress message is created/updated, and the `LastActivityID` cursor is advanced in Firestore.
*   **`TestPoller_SessionCompletedAndPR`**:
    *   **Scenario**: Poller detects a `SessionCompleted` activity, and the session outputs contain a Pull Request URL.
    *   **Verification**: Ensures the completion summary is sent to Telegram, a separate PR notification with a link is sent, and the chat state in Firestore is updated to `COMPLETED`.
*   **`TestPoller_SessionFailed`**:
    *   **Scenario**: Poller detects a `SessionFailed` activity.
    *   **Verification**: Ensures an error notification message containing the failure reason is sent to Telegram.

## Continuous Integration

Tests are automatically run on every `push` and `pull_request` to the `main` branch using GitHub Actions (`.github/workflows/test.yml`).

To run tests locally:
```bash
go test ./...
```
