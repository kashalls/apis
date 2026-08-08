# apis

A small self-hosted Go HTTP service, built from one repo and one binary,
that bundles four integrations, each under its own path prefix:

- **trmnl** (`/api/trmnl`) - endpoints to queue text or an image for a
  [TRMNL](https://usetrmnl.com) device's Polling-strategy private plugin.
- **homeassistant** (`/api/homeassistant`) - an endpoint to set a Home
  Assistant light (or light group)'s color.
- **spotify** (`/api/spotify`) - endpoints to read the currently playing
  song and recent listens from a Spotify account, after a one-time OAuth
  setup.
- **github** (`/api/github`) - endpoints to read a GitHub account's
  pinned repositories and contribution calendar.

> The Discord/Lanyard presence service that used to live here has moved to [kashalls/juno](https://github.com/kashalls/juno).

## How it works

**trmnl.** TRMNL private plugins can either be pushed to (Webhook strategy) or fetch content themselves (Polling strategy, where TRMNL issues a `GET` to a URL you give it and treats the response body's root-level JSON keys as merge variables). This service uses Polling for both content types:
- `POST /api/trmnl/text` accepts `{text, author}`, and `POST /api/trmnl/image` accepts either a `multipart/form-data` upload (stored and served back at a URL under `/api/trmnl/images/`) or a JSON `{"image_url": "..."}`. Neither talks to TRMNL directly - each publishes the resulting merge variables to an MQTT topic (`trmnl/text` / `trmnl/image`), which this service's own subscriber picks up and appends to a Redis-backed queue. Each is independently rate limited to 1 push per 5 minutes, to prevent flooding that queue.
- `GET /api/trmnl/text` and `GET /api/trmnl/image` - the URLs you configure as each plugin's Polling URL - serve the queue: if more than one message is pending, the next one in line is popped and returned (draining a backlog one poll at a time); if only one remains, it's served again without being removed, so the display doesn't go blank between real pushes; if nothing's ever been pushed, an empty object is returned.

**homeassistant.** `POST /api/homeassistant/color` sets a light (or light group) entity's color via the Home Assistant REST API, using a long-lived access token. Rate limited to 1 request per 2 seconds.

**spotify.** A one-time OAuth authorization-code flow (`GET /api/spotify/authorize` returns the Spotify consent URL, which redirects back to `GET /api/spotify/setup`) yields a refresh token, stored in Redis (`spotify/refresh_token`, plus `spotify/access_token` cached with a TTL matching Spotify's expiry). After that, a background poller refreshes the current playback state once a second and `GET /api/spotify/current` serves it from that in-memory cache (so the endpoint is instant and doesn't call Spotify per request); `GET /api/spotify/recents` proxies the recently-played history from the Spotify Web API directly.

Whenever the polled state actually changes (track, play/pause, or device - not just playback position ticking up), the same JSON is also published to the Redis channel `spotify/current` and retained to `MQTT_TOPIC` (default `spotify/current`) - so other processes (a frontend, Home Assistant, etc.) can react in real time instead of polling this API themselves.

**github.** `GET /api/github/pinned` and `GET /api/github/contributions` read from GitHub's GraphQL API using a personal access token. Unlike spotify's poller, these are cached lazily in Redis (`cache/github/pinned`, `cache/github/contributions`) for 30 minutes on a cache-aside basis: a request only calls GitHub when the cached value is missing or has expired. `GITHUB_USERNAME` picks whose pins to show; the contribution calendar always reflects whoever `GITHUB_TOKEN` belongs to (GitHub's GraphQL `viewer` field has no separate username parameter).

## One-time setup

### MQTT broker

Set `MQTT_BROKER_URL` to a broker reachable from this service (e.g. Mosquitto) - it's required, since trmnl's text/image push has no other delivery path (see below) and spotify's current-track changes publish here too. `MQTT_USERNAME`/`MQTT_PASSWORD` are optional if your broker needs auth.

### TRMNL private plugins (trmnl)

Create **two** private plugins in the TRMNL dashboard, both with strategy **Polling** (verb GET), so text and image each get their own queue and rate-limit quota:

1. **Text plugin**: create a Private Plugin, design its template to render `{{ text }}` / `{{ author }}`, set its strategy to Polling with the Polling URL `https://<your-host>/api/trmnl/text`.
2. **Image plugin**: create a second Private Plugin, design its template as `<img src="{{ image_url }}">`, set its strategy to Polling with the Polling URL `https://<your-host>/api/trmnl/image`.
3. Add each plugin to whichever playlist/device you want it to show up on.

### Home Assistant (homeassistant)

1. Set `HOME_ASSISTANT_BASE_URL` to your instance's base URL (e.g. `http://homeassistant.local:8123`).
2. Create a Long-Lived Access Token from your HA user profile → Security → Long-Lived Access Tokens, and copy it into `HOME_ASSISTANT_TOKEN`.
3. Set `HOME_ASSISTANT_LIGHT_GROUP` to the entity ID this service should control (e.g. `light.living_room`).

### Spotify (spotify)

1. Create an app at the [Spotify developer dashboard](https://developer.spotify.com/dashboard) and copy its Client ID and Client Secret into `SPOTIFY_CLIENT_ID` / `SPOTIFY_CLIENT_SECRET`.
2. Set `SPOTIFY_REDIRECT_URI` to the public URL of this service's `/api/spotify/setup` endpoint (e.g. `https://api.example.com/api/spotify/setup`) and add the exact same URL as a Redirect URI on the Spotify app.
3. Start the service, `GET /api/spotify/authorize`, and open the returned `url` in a browser while logged in to the Spotify account you want to track. Approving the consent screen redirects to `/api/spotify/setup`, which stores the tokens - you only do this once; refresh happens automatically afterwards.

### GitHub (github)

1. Set `GITHUB_USERNAME` to the account whose pinned repositories you want `/api/github/pinned` to show.
2. Create a personal access token (classic or fine-grained; no special scopes are needed to read public pins/contributions) and copy it into `GITHUB_TOKEN`. `/api/github/contributions` always reflects this token's own account.

## Configuration

Copy `.env.example` to `.env` and fill in the values (see comments in that file for details) - the service always loads and requires all four integrations' variables, plus `MQTT_BROKER_URL`.

```
PORT=8080
MQTT_BROKER_URL=
MQTT_USERNAME=
MQTT_PASSWORD=
MQTT_TOPIC=spotify/current
PUBLIC_BASE_URL=
DATA_DIR=/data
HOME_ASSISTANT_BASE_URL=
HOME_ASSISTANT_TOKEN=
HOME_ASSISTANT_LIGHT_GROUP=
SPOTIFY_CLIENT_ID=
SPOTIFY_CLIENT_SECRET=
SPOTIFY_REDIRECT_URI=
REDIS_URL=redis://redis:6379/0
GITHUB_USERNAME=
GITHUB_TOKEN=
TRUSTED_PROXY_CIDRS=
CORS_ALLOWED_ORIGINS=
```

`PUBLIC_BASE_URL` is only required if you plan to use the image-upload path of `/api/trmnl/image`; it's used to build the URL your uploaded image is served back at.

`TRUSTED_PROXY_CIDRS` is a comma-separated list of CIDRs for reverse proxies you trust to set `X-Forwarded-For` (e.g. `10.0.0.0/8`). Leave blank if the service is reachable directly, with no reverse proxy in front.

`CORS_ALLOWED_ORIGINS` is a comma-separated list of origins allowed to make cross-origin requests (e.g. if you call `/api/spotify/current` from a browser-based dashboard on another domain). Leave blank to use the default policy: any `ok8.sh` origin (apex or subdomain) or any origin on port 3000 (local dev).

## Running

```
docker compose up --build -d
```

This builds the image and starts the container, plus a `redis` container (with AOF persistence in a named volume) that stores spotify's OAuth tokens, the github cache, and trmnl's pending-message queues. trmnl's uploaded images persist to `./data`. By default the service listens on host port `8080`, with a healthcheck at `curl http://localhost:8080/healthz`. `docker-compose.yml` doesn't bundle an MQTT broker - point `MQTT_BROKER_URL` at one you already run.

You can also build the image directly with `docker build -t apis .`.

## API reference

### `GET /healthz`

Liveness check. Returns `{"status":"ok"}`.

### `GET /metrics`

Prometheus metrics in text exposition format.

### trmnl

#### `POST /api/trmnl/text`

```json
{"text": "Hello world", "author": "optional"}
```

Publishes to mqtt for the queue consumer to pick up. Returns `202 Accepted` on success, `429` (with `Retry-After`) if rate limited.

#### `POST /api/trmnl/image`

Either:

```
Content-Type: multipart/form-data
image=<file>   (png, jpeg, or webp; max 2MB)
```

or:

```json
{"image_url": "https://example.com/my-image.png"}
```

Publishes to mqtt for the queue consumer to pick up. Returns `202 Accepted` on success, `429` (with `Retry-After`) if rate limited.

#### `GET /api/trmnl/text` / `GET /api/trmnl/image`

The URLs to configure as each plugin's Polling URL in TRMNL. Returns the current merge_variables as a flat JSON object, e.g. `{"text": "Hello world", "author": "optional"}` or `{"image_url": "https://..."}`:

- More than one message queued: pops and returns the next one (drains a backlog one poll at a time).
- Exactly one message queued: returns it again without removing it, so the display doesn't go blank between pushes.
- Nothing ever pushed: returns `{}`.

### homeassistant

#### `POST /api/homeassistant/color`

```json
{"hex": "#ff8800"}
```

Sets the configured light group's color only (brightness is never accepted or forwarded). Returns `202 Accepted` on success, `429` (with `Retry-After`) if rate limited.

### spotify

Until the one-time setup is completed, `/api/spotify/current` and `/api/spotify/recents` return `503`.

#### `GET /api/spotify/current`

The currently playing track or podcast episode:

```json
{
  "playing": true,
  "id": "4uLU6hMCjMI75M1A2tKUQC",
  "type": "track",
  "name": "Never Gonna Give You Up",
  "artists": ["Rick Astley"],
  "length": 213573,
  "progress": 42000,
  "image": "https://i.scdn.co/image/...",
  "device": {"name": "Desktop", "type": "Computer"}
}
```

Returns `{"playing": false}` when nothing is playing.

#### `GET /api/spotify/recents?limit=10`

The most recently finished listens, newest first (`limit` is 1-50, default 10):

```json
{
  "recents": [
    {
      "id": "4uLU6hMCjMI75M1A2tKUQC",
      "type": "track",
      "name": "Never Gonna Give You Up",
      "artists": ["Rick Astley"],
      "length": 213573,
      "image": "https://i.scdn.co/image/...",
      "listened_at": "2026-07-30T12:34:56.789Z"
    }
  ]
}
```

#### `GET /api/spotify/authorize`

Starts the one-time OAuth setup: returns `{"url": "https://accounts.spotify.com/authorize?..."}` to open in a browser, or `400` if already authorized.

#### `GET /api/spotify/setup?code=...&state=...`

OAuth callback target (the `SPOTIFY_REDIRECT_URI`); Spotify redirects here after consent. Stores the tokens and returns `204`, or `400` on a missing code/state mismatch.

### github

Both endpoints are cached in Redis for 30 minutes; a request only calls GitHub when the cache is empty or expired. Both return `502` if the upstream GraphQL call fails.

#### `GET /api/github/pinned`

`GITHUB_USERNAME`'s first 6 pinned repositories:

```json
{
  "repositories": [
    {
      "owner": "kashalls",
      "name": "apis",
      "description": "Self-hosted integrations API",
      "stars": 3,
      "forks": 0,
      "language": "Go",
      "pushed_at": "2026-07-31T12:00:00Z",
      "url": "https://github.com/kashalls/apis"
    }
  ]
}
```

#### `GET /api/github/contributions`

`GITHUB_TOKEN`'s own contribution calendar for the year:

```json
{
  "graph": [
    [
      {"count": 0, "date": "2026-01-01"},
      {"count": 4, "date": "2026-01-02"}
    ]
  ],
  "total_contributions": 842
}
```
