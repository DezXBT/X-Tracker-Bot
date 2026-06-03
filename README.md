# X-Tracker-Bot

**Real-time X (Twitter) follow tracker with Discord alerts.** Monitor who influential accounts follow on X/Twitter — get instant Discord notifications when they follow someone new.

Written in **Go**. Single binary. Zero dependencies. ~6MB.

---

## What It Does

X-Tracker-Bot watches a list of X/Twitter accounts ("watchers") and detects new accounts they follow. When a watcher follows someone new, you get a Discord webhook alert with:

- **Who** followed **whom** (with profile links)
- **Follower count** of the new follow
- **Bio** of the new follow
- **Profile picture** thumbnail

Use cases:
- 🐦 **Alpha tracking** — see what KOLs/influencers follow early
- 📊 **Project discovery** — catch new projects before they trend
- 🔍 **Due diligence** — monitor competitor follow activity
- 🎯 **Signal detection** — find accounts followed by multiple watchers

---

## Features

| Feature | Description |
|---------|-------------|
| **Cookie Pool** | Multiple X auth cookies with round-robin rotation |
| **Auto-Dedup** | Duplicate accounts in watch list are automatically removed |
| **Skip Bad Users** | Suspended/deactivated accounts are skipped gracefully |
| **Graceful Shutdown** | `Ctrl+C` saves state cleanly before exit |
| **Warmup Baseline** | Existing follows are recorded first — no spam on startup |
| **Rate Limit Handling** | Auto-waits when X rate limits are hit |
| **Config Validation** | Catches errors at startup, not mid-run |
| **Structured Logging** | Timestamped logs with levels (debug/info/warn/error) |
| **Lightweight** | ~14MB RAM, single 6MB binary |

---

## Quick Start (3 Steps)

### 1. Download

```bash
git clone git@github.com:DezXBT/X-Tracker-Bot.git
cd X-Tracker-Bot
go build -ldflags="-s -w" -o x-tracker .
```

Or download a pre-built binary from [Releases](https://github.com/DezXBT/X-Tracker-Bot/releases).

### 2. Configure

```bash
cp config.example.yaml config.yaml
```

Edit `config.yaml`:

```yaml
twitter:
  cookies:
    - auth_token: "YOUR_AUTH_TOKEN"
      ct0: "YOUR_CT0"

discord:
  raw_webhooks:
    - "https://discord.com/api/webhooks/YOUR_WEBHOOK_URL"
```

### 3. Add Accounts & Run

```bash
# Add accounts to watch (one per line)
echo "https://x.com/elonmusk" >> twitter.txt

# Run
./x-tracker
```

---

## How to Get X Cookies

You need two cookies from X/Twitter:

1. **Login** to [x.com](https://x.com) in your browser
2. Open **DevTools** (F12) → **Application** → **Cookies** → `x.com`
3. Find and copy:
   - `auth_token` — long hex string
   - `ct0` — long hex string
4. Paste into `config.yaml`

> ⚠️ These cookies give full access to the X account. Use a dedicated/alt account, not your main.

### Multiple Cookies (Recommended)

Add multiple cookie pairs for round-robin rotation. This reduces rate limit risk:

```yaml
twitter:
  cookies:
    - auth_token: "account1_token"
      ct0: "account1_ct0"
    - auth_token: "account2_token"
      ct0: "account2_ct0"
```

---

## How to Get Discord Webhook URL

1. Open Discord → Go to your channel
2. **Edit Channel** → **Integrations** → **Webhooks**
3. Click **New Webhook**
4. Copy the webhook URL
5. Paste into `config.yaml` under `discord.raw_webhooks`

---

## `twitter.txt` Format

One account per line. Supports multiple formats:

```
https://x.com/NFTCPS
@SkyAAmen
0xtunglee
https://twitter.com/handle
```

Blank lines and lines starting with `#` are ignored. Duplicates are automatically removed.

---

## Config Reference

```yaml
# X/Twitter authentication
twitter:
  cookies:
    - auth_token: "xxx"
      ct0: "yyy"

# Path to watch accounts file
watch_file: twitter.txt

# Tracking settings
tracking:
  track_all_follows: true      # true = alert on ALL new follows
  poll_interval: 10m           # How often to scan (Go duration)
  page_size: 10                # Users per API page
  max_pages: 2                 # Max pages per watcher per scan
  page_delay: 500ms            # Delay between API pages

# Discord webhooks
discord:
  raw_webhooks:
    - "https://discord.com/api/webhooks/..."

# Logging
logging:
  timezone: Asia/Jakarta       # Timezone for log timestamps
  level: info                  # debug | info | warn | error
```

### Tracking Modes

| Mode | `track_all_follows` | Behavior |
|------|---------------------|----------|
| **Full** | `true` | Alert on every new follow from watchers |
| **Targeted** | `false` | Only alert when watchers follow accounts in `twitter.txt` |

---

## Running as a Service

### PM2 (Recommended)

```bash
pm2 start ./x-tracker --name x-tracker
pm2 save
pm2 startup
```

### systemd

```bash
sudo tee /etc/systemd/system/x-tracker.service << 'EOF'
[Unit]
Description=X-Tracker-Bot
After=network.target

[Service]
Type=simple
WorkingDirectory=/path/to/X-Tracker-Bot
ExecStart=/path/to/X-Tracker-Bot/x-tracker
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl enable x-tracker
sudo systemctl start x-tracker
```

### Screen (Quick)

```bash
screen -S x-tracker
./x-tracker
# Ctrl+A, D to detach
```

---

## How It Works

```
┌──────────────┐     ┌──────────────────┐     ┌──────────────┐
│  twitter.txt │────▶│   X-Tracker-Bot  │────▶│   Discord    │
│  (watch list)│     │                  │     │   Webhook    │
└──────────────┘     │  1. Warmup       │     └──────────────┘
                     │  2. Scan follows │
┌──────────────┐     │  3. Detect new   │     ┌──────────────┐
│  X (Twitter) │◀───▶│  4. Send alert   │     │ state.json   │
│  GraphQL API │     │  5. Save state   │────▶│ events.jsonl │
└──────────────┘     └──────────────────┘     └──────────────┘
```

1. **Warmup** — On first run, fetches all current follows as baseline
2. **Scan** — Every `poll_interval`, fetches latest follows for each watcher
3. **Detect** — Compares with previous baseline to find new follows
4. **Alert** — Sends Discord webhook embed for each new follow
5. **Save** — Persists state to `state/` directory

---

## State Files

| File | Purpose |
|------|---------|
| `state/state.json` | Baseline following + sent alert pairs |
| `state/events.jsonl` | Event log of all detected follows |

Delete the `state/` folder to reset (bot will re-warmup on next start).

---

## Troubleshooting

| Problem | Solution |
|---------|----------|
| `config invalid: no twitter cookies configured` | Fill in `auth_token` and `ct0` in `config.yaml` |
| `unauthorized (401)` | X cookies expired — get fresh ones |
| `rate limited (429)` | Add more cookies to the pool, or increase `poll_interval` |
| No alerts appearing | Check webhook URL is correct, check `twitter.txt` has accounts |
| `watch_file not found` | Make sure `twitter.txt` exists in the same directory |
| Bot sends old follows on restart | Delete `state/` folder and restart |

---

## Build from Source

```bash
# Requires Go 1.21+
git clone git@github.com:DezXBT/X-Tracker-Bot.git
cd X-Tracker-Bot
go build -ldflags="-s -w" -o x-tracker .
```

The `-ldflags="-s -w"` strips debug info for a smaller binary (~6MB).

### Cross-compile

```bash
# Linux ARM64
GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o x-tracker-linux-arm64 .

# macOS
GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o x-tracker-macos .

# Windows
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o x-tracker.exe .
```

---

## Tech Stack

- **Language:** Go (single binary, no runtime dependencies)
- **API:** X/Twitter internal GraphQL API (cookie-based auth)
- **Output:** Discord webhooks (embeds)
- **State:** JSON file persistence
- **Config:** YAML with validation

---

## License

MIT

---

## Keywords

x tracker, twitter follow tracker, twitter follow alert, discord webhook, kol tracking, alpha tracker, x follow monitor, twitter bot, x bot, follow notification, early tracking, twitter follow discord, x follow discord, crypto twitter tracker, kol follow alert, twitter follow notification bot, x follow notification bot
