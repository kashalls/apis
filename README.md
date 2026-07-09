# juno

Self-hosted Go API with two capabilities:

- A [Lanyard](https://github.com/Phineas/lanyard)-compatible REST + WebSocket feed of your live Discord status, scoped to a single Discord user.
- Endpoints to push text or an image to a [TRMNL](https://usetrmnl.com) device via its private-plugin webhook.

## How it works

**Discord status.** A bot account joins a server you're also in and, with the Presence and Server Members privileged intents enabled, receives real-time presence updates from Discord's Gateway for your user ID. Juno caches your latest presence in memory and serves it over REST and a Lanyard-shaped WebSocket protocol.

**TRMNL.** TRMNL's private-plugin webhook only accepts a JSON body of `merge_variables`, rendered through a template you configure once in the TRMNL dashboard — it does not accept raw image bytes. Juno uses two separate private plugins (and webhooks), one per content type, so each gets its own independent TRMNL rate limit instead of sharing one pool:
- `POST /api/trmnl/text` sends `{text, author}` as merge variables to `TRMNL_TEXT_WEBHOOK_URL`.
- `POST /api/trmnl/image` accepts either a `multipart/form-data` upload (juno stores it and serves it back at a URL under `/images/`) or a JSON `{"image_url": "..."}` if you already have a publicly reachable image URL. Either way, the resulting URL is sent as the `image_url` merge variable to `TRMNL_IMAGE_WEBHOOK_URL` for your plugin template to render (e.g. `<img src="{{ image_url }}">`).
- Each endpoint is independently rate limited to 1 request per 5 minutes by Juno itself, on top of whatever TRMNL's own per-plugin webhook limit is (12x/hour standard, 30x/hour on TRMNL+).

## One-time setup

### Discord bot

1. Create an application at the [Discord Developer Portal](https://discord.com/developers/applications) and add a Bot user.
2. Under Bot settings, enable the **Presence Intent** and **Server Members Intent** privileged intents.
3. Invite the bot to any server you're also a member of (OAuth2 URL Generator, `bot` scope, no permissions needed).
4. Copy the bot token into `DISCORD_BOT_TOKEN`.
5. Enable Developer Mode in Discord, right-click your own name, and copy your user ID into `DISCORD_USER_ID`.

### TRMNL private plugins

Create **two** private plugins in the TRMNL dashboard, so text and image pushes each get their own webhook and rate-limit quota:

1. **Text plugin**: create a Private Plugin, design its template to render `{{ text }}` / `{{ author }}`, then copy its **Webhook URL** (looks like `https://usetrmnl.com/api/custom_plugins/<uuid>`) into `TRMNL_TEXT_WEBHOOK_URL`.
2. **Image plugin**: create a second Private Plugin, design its template as `<img src="{{ image_url }}">`, then copy its Webhook URL into `TRMNL_IMAGE_WEBHOOK_URL`.
3. Add each plugin to whichever playlist/device you want it to show up on.

## Configuration

Copy `.env.example` to `.env` and fill in the values (see comments in that file for details):

```
PORT=8080
DISCORD_BOT_TOKEN=
DISCORD_USER_ID=
DISCORD_GUILD_ID=
TRMNL_TEXT_WEBHOOK_URL=
TRMNL_IMAGE_WEBHOOK_URL=
PUBLIC_BASE_URL=
DATA_DIR=/data
```

`PUBLIC_BASE_URL` is only required if you plan to use the image-upload path of `/api/trmnl/image`; it's used to build the URL your uploaded image is served back at.

## Running

```
docker compose up --build -d
```

This builds the image, starts the container, and persists uploaded images to `./data`. Check `curl http://localhost:8080/healthz` once it's up.

## API reference

### `GET /healthz`

Liveness check. Returns `{"status":"ok"}`.

### `GET /v1/users/{discord_user_id}`

Returns your cached presence if `{discord_user_id}` matches `DISCORD_USER_ID`, otherwise `404`.

```json
{
  "success": true,
  "data": {
    "discord_user": {"id": "...", "username": "...", "discriminator": "0", "global_name": "...", "avatar": "..."},
    "discord_status": "online",
    "activities": [ /* raw Discord activity objects */ ],
    "listening_to_spotify": false,
    "spotify": null,
    "active_on_discord_desktop": true,
    "active_on_discord_mobile": false,
    "active_on_discord_web": false
  }
}
```

### `GET /socket`

WebSocket endpoint using Lanyard's own protocol:

- Server sends `{"op":1,"d":{"heartbeat_interval":30000}}` (Hello) on connect.
- Client may send `{"op":2,"d":{"subscribe_to_id":"<your id>"}}` (Initialize) — optional, since there's only ever one tracked user.
- Server sends `{"op":0,"t":"INIT_STATE","d":<presence>}` immediately after connecting, then `{"op":0,"t":"PRESENCE_UPDATE","d":<presence>}` on every subsequent change.
- Client should send `{"op":3}` (Heartbeat) periodically to keep the connection alive.

### `POST /api/trmnl/text`

```json
{"text": "Hello from Juno", "author": "optional"}
```

Returns `202 Accepted` on success, `429` (with `Retry-After`) if rate limited.

### `POST /api/trmnl/image`

Either:

```
Content-Type: multipart/form-data
image=<file>   (png, jpeg, or webp; max 2MB)
```

or:

```json
{"image_url": "https://example.com/my-image.png"}
```

Returns `202 Accepted` on success, `429` (with `Retry-After`) if rate limited.
