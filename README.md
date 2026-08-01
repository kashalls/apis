# apis

A small self-hosted Go HTTP service, built from one repo and one binary,
that bundles four integrations, each under its own path prefix:

- **trmnl** (`/api/trmnl`) - endpoints to push text or an image to a
  [TRMNL](https://usetrmnl.com) device via its private-plugin webhook.
- **homeassistant** (`/api/homeassistant`) - an endpoint to set a Home
  Assistant light (or light group)'s color.
- **spotify** (`/api/spotify`) - endpoints to read the currently playing
  song and recent listens from a Spotify account, after a one-time OAuth
  setup.
- **github** (`/api/github`) - endpoints to read a GitHub account's
  pinned repositories and contribution calendar.

> The Discord/Lanyard presence service that used to live here has moved to [kashalls/juno](https://github.com/kashalls/juno).

## How it works

**trmnl.** TRMNL's private-plugin webhook only accepts a JSON body of `merge_variables`, rendered through a template you configure once in the TRMNL dashboard — it does not accept raw image bytes. This service uses two separate private plugins (and webhooks), one per content type, so each gets its own independent TRMNL rate limit instead of sharing one pool:
- `POST /api/trmnl/text` sends `{text, author}` as merge variables to `TRMNL_TEXT_WEBHOOK_URL`.
- `POST /api/trmnl/image` accepts either a `multipart/form-data` upload (stored and served back at a URL under `/api/trmnl/images/`) or a JSON `{"image_url": "..."}` if you already have a publicly reachable image URL. Either way, the resulting URL is sent as the `image_url` merge variable to `TRMNL_IMAGE_WEBHOOK_URL` for your plugin template to render (e.g. `<img src="{{ image_url }}">`).
- Each endpoint is independently rate limited to 1 request per 5 minutes on top of whatever TRMNL's own per-plugin webhook limit is (12x/hour standard, 30x/hour on TRMNL+).

**homeassistant.** `POST /api/homeassistant/color` sets a light (or light group) entity's color via the Home Assistant REST API, using a long-lived access token. Rate limited to 1 request per 2 seconds.

**spotify.** A one-time OAuth authorization-code flow (`GET /api/spotify/authorize` returns the Spotify consent URL, which redirects back to `GET /api/spotify/setup`) yields a refresh token, stored in Redis (`spotify/refresh_token`, plus `spotify/access_token` cached with a TTL matching Spotify's expiry). After that, a background poller refreshes the current playback state once a second and `GET /api/spotify/current` serves it from that in-memory cache (so the endpoint is instant and doesn't call Spotify per request); `GET /api/spotify/recents` proxies the recently-played history from the Spotify Web API directly.

**github.** `GET /api/github/pinned` and `GET /api/github/contributions` read from GitHub's GraphQL API using a personal access token. Unlike spotify's poller, these are cached lazily in Redis (`cache/github/pinned`, `cache/github/contributions`) for 30 minutes on a cache-aside basis: a request only calls GitHub when the cached value is missing or has expired. `GITHUB_USERNAME` picks whose pins to show; the contribution calendar always reflects whoever `GITHUB_TOKEN` belongs to (GitHub's GraphQL `viewer` field has no separate username parameter).

## One-time setup

### TRMNL private plugins (trmnl)

Create **two** private plugins in the TRMNL dashboard, so text and image pushes each get their own webhook and rate-limit quota:

1. **Text plugin**: create a Private Plugin, design its template to render `{{ text }}` / `{{ author }}`, then copy its **Webhook URL** (looks like `https://usetrmnl.com/api/custom_plugins/<uuid>`) into `TRMNL_TEXT_WEBHOOK_URL`.
2. **Image plugin**: create a second Private Plugin, design its template as `<img src="{{ image_url }}">`, then copy its Webhook URL into `TRMNL_IMAGE_WEBHOOK_URL`.
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

Copy `.env.example` to `.env` and fill in the values (see comments in that file for details) - the service always loads and requires all four integrations' variables.

```
PORT=8080
TRMNL_TEXT_WEBHOOK_URL=
TRMNL_IMAGE_WEBHOOK_URL=
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

This builds the image and starts the container, plus a `redis` container (with AOF persistence in a named volume) that spotify stores its OAuth tokens in. trmnl's uploaded images persist to `./data`. By default the service listens on host port `8080`, with a healthcheck at `curl http://localhost:8080/healthz`.

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

Returns `202 Accepted` on success, `429` (with `Retry-After`) if rate limited.

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

Returns `202 Accepted` on success, `429` (with `Retry-After`) if rate limited.

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
