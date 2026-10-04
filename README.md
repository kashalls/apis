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
- **wow** (`/api/wow`) - an endpoint to read World of Warcraft character
  summaries (retail and classic) from the Blizzard Profile API.
- **overwatch** (`/api/overwatch`) - an endpoint to read Overwatch career
  summaries via [OverFast](https://overfast-api.tekrop.fr).

> The Discord/Lanyard presence service that used to live here has moved to [kashalls/juno](https://github.com/kashalls/juno).

## How it works

**trmnl.** TRMNL's private-plugin webhook only accepts a JSON body of `merge_variables`, rendered through a template you configure once in the TRMNL dashboard — it does not accept raw image bytes. This service uses two separate private plugins (and webhooks), one per content type, so each gets its own independent TRMNL rate limit instead of sharing one pool:
- `POST /api/trmnl/text` sends `{text, author}` as merge variables to `TRMNL_TEXT_WEBHOOK_URL`.
- `POST /api/trmnl/image` accepts either a `multipart/form-data` upload (stored and served back at a URL under `/api/trmnl/images/`) or a JSON `{"image_url": "..."}` if you already have a publicly reachable image URL. Either way, the resulting URL is sent as the `image_url` merge variable to `TRMNL_IMAGE_WEBHOOK_URL` for your plugin template to render (e.g. `<img src="{{ image_url }}">`).
- Each endpoint is independently rate limited to 1 request per 5 minutes on top of whatever TRMNL's own per-plugin webhook limit is (12x/hour standard, 30x/hour on TRMNL+).

**homeassistant.** `POST /api/homeassistant/color` sets a light (or light group) entity's color via the Home Assistant REST API, using a long-lived access token. Rate limited to 1 request per 2 seconds.

**spotify.** A one-time OAuth authorization-code flow (`GET /api/spotify/authorize` returns the Spotify consent URL, which redirects back to `GET /api/spotify/setup`) yields a refresh token, stored in Redis (`spotify/refresh_token`, plus `spotify/access_token` cached with a TTL matching Spotify's expiry). After that, a background poller refreshes the current playback state once a second and `GET /api/spotify/current` serves it from that in-memory cache (so the endpoint is instant and doesn't call Spotify per request); `GET /api/spotify/recents` proxies the recently-played history from the Spotify Web API directly.

Whenever the polled state actually changes (track, play/pause, or device - not just playback position ticking up), the same JSON is also published to the Redis channel `spotify/current`, and, if `MQTT_BROKER_URL` is set, retained to `MQTT_TOPIC` (default `spotify/current`) - so other processes (a frontend, Home Assistant, etc.) can react in real time instead of polling this API themselves.

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

### World of Warcraft (wow)

1. Create a client at the [Blizzard developer portal](https://develop.battle.net/access/clients) and copy its ID and secret into `BLIZZARD_CLIENT_ID` / `BLIZZARD_CLIENT_SECRET`. The client-credentials token is cached in Redis (`wow/access_token`).
2. List characters in `WOW_CHARACTERS` as `flavor/realm-slug/name`, e.g. `retail/area-52/kash,classicann/dreamscythe/kash`. Each is cached for 30 minutes (`cache/wow/character/...`); one that fails to load is logged and left out.

### Overwatch (overwatch)

Blizzard has no official Overwatch API, so this reads [OverFast](https://overfast-api.tekrop.fr), which scrapes public career pages. Set `OVERWATCH_BATTLETAGS` and make sure the in-game career profile is public, or ranks and stats come back empty. Each player is cached for 30 minutes.

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
MQTT_BROKER_URL=
MQTT_USERNAME=
MQTT_PASSWORD=
MQTT_TOPIC=spotify/current
GITHUB_USERNAME=
GITHUB_TOKEN=
BLIZZARD_CLIENT_ID=
BLIZZARD_CLIENT_SECRET=
BLIZZARD_REGION=us
WOW_CHARACTERS=
OVERWATCH_BATTLETAGS=
TRUSTED_PROXY_CIDRS=
CORS_ALLOWED_ORIGINS=
```

`PUBLIC_BASE_URL` is only required if you plan to use the image-upload path of `/api/trmnl/image`; it's used to build the URL your uploaded image is served back at.

`MQTT_BROKER_URL` is optional - spotify's current-track changes are always published to the Redis channel `spotify/current` regardless, but setting a broker URL (e.g. `tcp://mosquitto:1883`) additionally retains the same JSON on `MQTT_TOPIC`. Leave it blank to skip MQTT entirely; nothing else in the app requires a broker.

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

### wow

#### `GET /api/wow/characters`

```json
{
  "characters": [
    {
      "flavor": "retail",
      "name": "Kash",
      "realm": "Area 52",
      "level": 90,
      "race": "Void Elf",
      "class": "Mage",
      "spec": "Frost",
      "faction": "Alliance",
      "guild": "Puddle",
      "item_level": 712,
      "achievement_points": 15000,
      "last_login": 1791000000000,
      "mythic_rating": {"rating": 2512.4, "color": "#ff8000"},
      "media": {"avatar": "https://...", "inset": "https://...", "main": "https://..."}
    }
  ]
}
```

`mythic_rating` is retail only and omitted without keystone runs; `media` fields are omitted when Blizzard doesn't provide them.

### overwatch

#### `GET /api/overwatch/players`

```json
{
  "players": [
    {
      "battletag": "TeKrop-2217",
      "username": "TeKrop",
      "title": "Data Broker",
      "avatar": "https://...",
      "namecard": "https://...",
      "endorsement": 2,
      "season": 22,
      "ranks": [
        {"role": "support", "division": "silver", "tier": 4, "rank_icon": "https://...", "role_icon": "https://..."}
      ],
      "stats": {"games_played": 12830, "games_won": 6601, "time_played": 5935880, "winrate": 51.45, "kda": 3.01},
      "top_heroes": [
        {"hero": "reinhardt", "time_played": 1336548, "winrate": 53.65}
      ]
    }
  ]
}
```

`stats` is PC only and omitted for private profiles; `time_played` is in seconds.
