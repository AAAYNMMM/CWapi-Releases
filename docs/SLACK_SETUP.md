# CWapi 1.6.3 Slack Setup

[English](SLACK_SETUP.md) | [简体中文](SLACK_SETUP.zh-CN.md)

CWapi 1.6.3 uses Slack Socket Mode as its remote control and result transport. It does not need a public Events API Request URL.

## Two different Slack connections

Keep these separate:

```text
A. CWapi's Slack App/Bot
   Used by the local CWapi process to receive requests, post responses, and upload files.

B. ChatGPT's Slack connection
   Used by Web GPT to send requests and read CWapi responses in the control channel.
```

The CWapi `xapp-...` and `xoxb-...` tokens belong only in the local CWapi configuration. Never paste them into Web GPT.

## Recommended public-channel setup

Create a dedicated public channel such as `#cwapi-control`.

### App-Level Token scope

```text
connections:write
```

### Bot Token scopes

```text
channels:read
channels:history
chat:write
files:write
```

### Bot event

```text
message.channels
```

These scopes map to the main workflow as follows:

```text
Socket Mode connection -> connections:write
Discover/read channel  -> channels:read
Read history            -> channels:history
Post response           -> chat:write
Upload screenshot/file  -> files:write
Receive public messages -> message.channels
```

## Create the Slack App

1. Open `https://api.slack.com/apps`.
2. Choose **Create New App -> From scratch**.
3. Give it a name such as `CWapi`.
4. Select the target Workspace.

## Add Bot Token scopes

Open:

```text
OAuth & Permissions
-> Bot Token Scopes
```

Add:

```text
channels:read
channels:history
chat:write
files:write
```

Whenever scopes change, reinstall the app to the Workspace so the token receives the new grants.

## Enable Event Subscriptions

Open:

```text
Event Subscriptions
-> Enable Events
-> Subscribe to bot events
```

Add:

```text
message.channels
```

## Enable Socket Mode and create the App Token

Enable **Socket Mode**, then open:

```text
Basic Information
-> App-Level Tokens
-> Generate Token and Scopes
```

Add:

```text
connections:write
```

Generate the `xapp-...` token.

## Install the app and get the Bot Token

Open:

```text
OAuth & Permissions
-> Install to Workspace
```

After installation, copy:

```text
Bot User OAuth Token = xoxb-...
```

## Create the control channel and add the bot

Create a dedicated channel and add the CWapi bot to it.

CWapi uses the Channel ID, not the display name. A Slack browser URL commonly looks like:

```text
https://app.slack.com/client/T01234567/C0123456789
```

The final `C0123456789` part is the Channel ID.

## Save credentials in CWapi

In the Slack configuration area enter:

```text
App Token   = xapp-...
Bot Token   = xoxb-...
Channel ID  = C...
```

CWapi validates candidate credentials before saving them. App/Bot tokens go to the current Windows user's Credential Manager. The Channel ID is the Slack field persisted in the JSON config.

## Verify the connection

The Slack component should become healthy/connected and show the configured channel.

A green basic state is not proof that every real action has been exercised. The first end-to-end test should verify:

- receiving a message;
- reading the request/history as needed;
- posting a response;
- uploading a file if your workflow needs files.

## First protocol test

CWapi 1.6.3 has no project registry and no `projects/list`.

A simple global test is `mcpServerStatus/list` with empty params. The message is transported through the configured Slack channel as a `[CWapi/MCP/2]` request and CWapi replies with `MCP_RESPONSE`.

Repository requests then add the GitHub `repository_url` and full 40-character `expected_commit` at the outer request level.

## Private channels

For a private Slack channel, also add:

```text
groups:read
groups:history
```

and subscribe to:

```text
message.groups
```

Reinstall the app after scope changes and make sure the bot is explicitly added to the private channel.

## Common Slack errors

### `SLACK_APP_TOKEN_INVALID`

The value is not a valid `xapp-...` token or contains accidental whitespace.

### `SLACK_BOT_TOKEN_INVALID`

The value is not a valid `xoxb-...` Bot User OAuth Token.

### `SLACK_READINESS_BOT_IDENTITY_FAILED`

The Bot Token is revoked/invalid, belongs to the wrong Workspace, or Slack API access failed.

### `SLACK_READINESS_CHANNEL_FAILED`

Check the Channel ID, channel read scopes, and whether the bot is actually in the channel.

### `SLACK_BOT_NOT_CHANNEL_MEMBER`

Invite the bot to the configured control channel.

### `SLACK_READINESS_SOCKET_FAILED`

Check Socket Mode, the App Token, and `connections:write`.

### `SLACK_API_ERROR_missing_scope`

Add the missing scope and reinstall the app to the Workspace.

### Text works but screenshot/file upload fails

Check `files:write`. For Playwright screenshots, call `browser_take_screenshot` without a `filename` so actual image bytes are returned to CWapi.

## Token safety

Treat `xapp-...` and `xoxb-...` like passwords. Do not commit them, paste them into ChatGPT, include them in CWapi command arguments, post them in issues, or expose them in screenshots/logs.

If a token is exposed, revoke/rotate it in Slack. Deleting the message that leaked it does not revoke the credential.
