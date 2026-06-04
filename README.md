<!--
X-Tracker-Bot — Real-time X (Twitter) follow tracker with Discord alerts.
Keywords: x tracker, twitter follow tracker, twitter follow alert bot, discord
webhook notifier, kol tracker, alpha tracker, crypto twitter monitor, x follow
notification, twitter monitoring tool, follow tracking bot, go twitter bot.
-->

# X-Tracker-Bot — Real-Time X (Twitter) Follow Tracker with Discord Alerts

> **X-Tracker-Bot** monitors who your chosen X / Twitter accounts follow and sends you an **instant Discord notification** the moment they follow someone new. Lightweight, single-binary, written in **Go**.

<p align="center">
  <img alt="Language: Go" src="https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go&logoColor=white">
  <img alt="Platform" src="https://img.shields.io/badge/platform-Linux%20%7C%20macOS%20%7C%20Windows-lightgrey">
  <img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-green">
  <img alt="Binary size" src="https://img.shields.io/badge/binary-~6MB-blue">
  <img alt="RAM usage" src="https://img.shields.io/badge/RAM-~14MB-blue">
</p>

**X-Tracker-Bot** is a free, open-source tool that tracks new follows on X (formerly Twitter) and pushes real-time alerts to Discord via webhooks. It's perfect for **alpha hunting, KOL tracking, project discovery, and competitor research** — see what influential accounts follow *before* everyone else.

---

## 📋 Table of Contents

- [What Is X-Tracker-Bot?](#-what-is-x-tracker-bot)
- [Key Features](#-key-features)
- [What You Need Before Starting](#-what-you-need-before-starting)
- [Installation Guide (Step by Step)](#-installation-guide-step-by-step)
  - [Step 1 — Install Go](#step-1--install-go)
  - [Step 2 — Download X-Tracker-Bot](#step-2--download-x-tracker-bot)
  - [Step 3 — Build the Bot](#step-3--build-the-bot)
  - [Step 4 — Create Your Config File](#step-4--create-your-config-file)
  - [Step 5 — Get Your X (Twitter) Cookies](#step-5--get-your-x-twitter-cookies)
  - [Step 6 — Get Your Discord Webhook URL](#step-6--get-your-discord-webhook-url)
  - [Step 7 — Add Accounts to Watch](#step-7--add-accounts-to-watch)
  - [Step 8 — Run the Bot](#step-8--run-the-bot)
  - [Step 9 — (Optional) Enable AI Categorization & Hourly Summary](#step-9--optional-enable-ai-categorization--hourly-summary)
- [Configuration Reference](#-configuration-reference)
- [Running 24/7 (Keep It Always On)](#-running-247-keep-it-always-on)
- [How It Works](#-how-it-works)
- [Troubleshooting](#-troubleshooting)
- [FAQ](#-frequently-asked-questions-faq)
- [Build from Source & Cross-Compile](#-build-from-source--cross-compile)
- [Tech Stack](#-tech-stack)
- [License](#-license)

---

## 🎯 What Is X-Tracker-Bot?

X-Tracker-Bot is a **follow-tracking bot for X / Twitter**. You give it a list of accounts to watch ("watchers"), and it continuously checks who those accounts follow. Whenever a watched account follows someone new, the bot sends a rich **Discord alert** containing:

- ✅ **Who** followed **whom** (with clickable X profile links)
- ✅ The new account's **follower count**
- ✅ The new account's **bio**
- ✅ The new account's **project category** (AI-powered — see below)
- ✅ The new account's **profile picture** thumbnail
- ✅ A precise **timestamp** in your timezone

On top of the per-follow alerts, the bot can post an **hourly summary** that groups every new follow by **project category** (AI, Layer 2, DeFi, NFT, …) and shows how many of your watched accounts followed each target — so you instantly see what the crowd is piling into.

**Common use cases:**

| Use Case | Description |
|----------|-------------|
| 🐦 **Alpha tracking** | See what KOLs and influencers follow early |
| 📊 **Project discovery** | Catch new projects before they trend |
| 🔍 **Due diligence** | Monitor competitor or partner follow activity |
| 🎯 **Signal detection** | Spot accounts followed by multiple watchers |

---

## ✨ Key Features

| Feature | Description |
|---------|-------------|
| **Real-Time Discord Alerts** | Rich embeds with profile link, bio, followers & avatar |
| **AI Categorization** | Tags each followed account by project category (AI, Layer 2, DeFi, NFT…) via OpenRouter LLM + keyword fallback |
| **Hourly Summary** | Posts a periodic digest grouped by category, counting how many watchers followed each target |
| **Dynamic Query IDs** | Pulls X's GraphQL query IDs live at startup, so the bot keeps working when X rotates them |
| **Smart Category Cache** | Caches each account's category (7-day TTL) to conserve OpenRouter free-tier quota |
| **Cookie Pool Rotation** | Use multiple X auth cookies with round-robin rotation to reduce rate limits |
| **Auto Dedup** | Duplicate accounts in your watch list are removed automatically |
| **Skip Bad Users** | Suspended / deactivated accounts are skipped gracefully |
| **Warmup Baseline** | Existing follows are recorded first — no notification spam on startup |
| **Rate-Limit Handling** | Automatically waits and rotates cookies when X rate-limits you |
| **Graceful Shutdown** | `Ctrl + C` saves state cleanly before exiting |
| **Config Validation** | Catches mistakes at startup, not mid-run |
| **Structured Logging** | Timestamped logs with levels (debug / info / warn / error) |
| **Ultra Lightweight** | Single ~6 MB binary, ~14 MB RAM, zero runtime dependencies |

---

## 🧰 What You Need Before Starting

Before installing, make sure you have:

1. **A computer or server** running Linux, macOS, or Windows.
2. **An X / Twitter account** (use a spare/alt account — see the warning in [Step 5](#step-5--get-your-x-twitter-cookies)).
3. **A Discord server** where you have permission to create a webhook.
4. About **10 minutes** ⏱️.

> 💡 **New to the command line?** Don't worry. Just copy and paste each command below, one at a time, and press Enter. The guide explains every step.

---

## 🚀 Installation Guide (Step by Step)

This guide is written for beginners. Follow each step in order.

### Step 1 — Install Go

X-Tracker-Bot is built with **Go** (version 1.24 or newer). If you don't have Go installed yet, install it:

**Linux (Ubuntu / Debian):**
```bash
sudo apt update && sudo apt install -y golang-go git
```

**macOS (with Homebrew):**
```bash
brew install go git
```

**Windows:**
Download and run the installer from [https://go.dev/dl/](https://go.dev/dl/), then install [Git for Windows](https://git-scm.com/download/win).

**Verify the installation:**
```bash
go version
```
You should see something like `go version go1.24.3`. ✅

---

### Step 2 — Download X-Tracker-Bot

Clone the repository to your computer:

```bash
git clone https://github.com/DezXBT/X-Tracker-Bot.git
cd X-Tracker-Bot
```

> 📦 Prefer not to build? Grab a ready-made binary from the [Releases page](https://github.com/DezXBT/X-Tracker-Bot/releases) and skip to **Step 4**.

---

### Step 3 — Build the Bot

Compile the bot into a single executable file:

```bash
go build -ldflags="-s -w" -o x-tracker .
```

This creates an `x-tracker` file (about 6 MB) in the folder. The `-ldflags="-s -w"` flags strip debug info to keep it small.

---

### Step 4 — Create Your Config File

Copy the example config to create your own:

```bash
cp config.example.yaml config.yaml
```

You'll edit `config.yaml` in the next steps to add your X cookies and Discord webhook.

---

### Step 5 — Get Your X (Twitter) Cookies

The bot logs into X using two browser cookies: `auth_token` and `ct0`.

1. **Log in** to [x.com](https://x.com) in your web browser.
2. Open **Developer Tools** by pressing `F12` (or right-click → *Inspect*).
3. Go to the **Application** tab → **Cookies** → `https://x.com`.
4. Find and copy these two values:
   - **`auth_token`** — a long hex string
   - **`ct0`** — a long hex string
5. Open `config.yaml` and paste them in:

```yaml
twitter:
  cookies:
    - auth_token: "PASTE_YOUR_AUTH_TOKEN_HERE"
      ct0: "PASTE_YOUR_CT0_HERE"
```

> ⚠️ **Security warning:** These cookies grant full access to the X account. **Use a dedicated alt account, never your main account.** Never share your config file or commit it to GitHub.

**Optional but recommended — add multiple cookies** to spread requests across accounts and avoid rate limits:

```yaml
twitter:
  cookies:
    - auth_token: "account1_token"
      ct0: "account1_ct0"
    - auth_token: "account2_token"
      ct0: "account2_ct0"
```

---

### Step 6 — Get Your Discord Webhook URL

1. In Discord, open the channel where you want alerts.
2. Click the ⚙️ **Edit Channel** → **Integrations** → **Webhooks**.
3. Click **New Webhook**, give it a name (e.g. *X-Tracker*), then **Copy Webhook URL**.
4. Paste it into `config.yaml`:

```yaml
discord:
  raw_webhooks:
    - "https://discord.com/api/webhooks/YOUR_WEBHOOK_URL_HERE"
```

---

### Step 7 — Add Accounts to Watch

Open the `twitter.txt` file and add the X accounts you want to monitor — **one per line**. All of these formats work:

```
https://x.com/elonmusk
@SkyAAmen
0xtunglee
https://twitter.com/handle
```

- Blank lines and lines starting with `#` are ignored.
- Duplicate accounts are removed automatically.

Quick way to add one from the command line:
```bash
echo "https://x.com/elonmusk" >> twitter.txt
```

---

### Step 8 — Run the Bot

You're ready! Start the bot:

```bash
./x-tracker
```

On the **first run**, the bot records everyone your watched accounts *already* follow (the "warmup baseline") — so you won't get spammed. After that, it checks every 10 minutes (configurable) and alerts you on **new** follows only. 🎉

Press `Ctrl + C` to stop the bot — it saves its state cleanly before exiting.

---

### Step 9 — (Optional) Enable AI Categorization & Hourly Summary

> 💡 **This step is optional.** Skip it and the bot works exactly like the classic version (raw alerts only). Turn it on to label every follow by **project category** and get an **hourly digest**.

The bot can categorize each followed account (AI, Layer 2, DeFi, NFT, Meme…) using a Large Language Model via **[OpenRouter](https://openrouter.ai)** — which offers **free-tier models**. Here's how to set it up:

**1. Get one (or more) OpenRouter API keys**

1. Sign up at [openrouter.ai](https://openrouter.ai).
2. Go to **Keys** → **Create Key** and copy it (starts with `sk-or-v1-...`).
3. *(Recommended)* Create **several keys** — the bot rotates them round-robin to spread the free-tier quota.

**2. Add a summary webhook** (a second Discord webhook, see [Step 6](#step-6--get-your-discord-webhook-url)) — ideally a **separate channel** so digests don't clutter your real-time alerts.

**3. Fill in the `categorization` and `summary` settings in `config.yaml`:**

```yaml
discord:
  raw_webhooks:
    - "https://discord.com/api/webhooks/REALTIME_ALERTS"
  summary_webhook: "https://discord.com/api/webhooks/HOURLY_SUMMARY"  # leave empty to disable summaries
  summary_interval: 1h            # how often to post the digest

categorization:
  enabled: true
  use_tweets: true                # also read recent tweets as a signal (1 extra API call per new account)
  tweet_count: 5
  cache_ttl: 168h                 # remember a category for 7 days (saves quota)
  categories:                     # base taxonomy — the LLM may add new ones when nothing fits
    - AI
    - Layer 1
    - Layer 2
    - DeFi
    - NFT
    - Gaming
    - Meme
    - DePIN
    - RWA
    - Infra
    - Social
  openrouter:
    api_keys:                     # multiple keys are rotated round-robin
      - "sk-or-v1-YOUR_KEY"
    models:                       # free-tier models, tried in order until one works
      - "meta-llama/llama-3.3-70b-instruct:free"
      - "google/gemini-2.0-flash-exp:free"
      - "deepseek/deepseek-chat-v3-0324:free"
```

**How it degrades gracefully:**

| Situation | What happens |
|-----------|--------------|
| No OpenRouter keys | Falls back to **keyword matching** (still tags obvious categories) |
| All LLM models fail / quota hit | Falls back to keyword matching, then `Uncategorized` |
| `summary_webhook` empty | Summaries are disabled; raw alerts still work |
| `enabled: false` | Categorization off entirely; behaves like the classic bot |

> ⚠️ **Free-tier note:** OpenRouter's free models have daily limits, and the available model names change over time. List a few models as fallbacks, add multiple keys, and keep `cache_ttl` high so the same account isn't re-queried. If a model 404s, the bot just tries the next one.

---

## ⚙️ Configuration Reference

All settings live in `config.yaml`:

```yaml
# X / Twitter authentication
twitter:
  cookies:
    - auth_token: "xxx"
      ct0: "yyy"

# Path to the file listing accounts to watch
watch_file: twitter.txt

# Tracking behavior
tracking:
  track_all_follows: true      # true = alert on ALL new follows
                               # false = only alert when a watcher follows
                               #         an account also listed in twitter.txt
  poll_interval: 10m           # How often to scan (Go duration: 30s, 10m, 1h)
  page_size: 10                # Users fetched per API page
  max_pages: 2                 # Max pages per watcher per scan
  page_delay: 500ms            # Delay between API pages

# Discord webhook(s)
discord:
  raw_webhooks:                # real-time, per-follow alerts
    - "https://discord.com/api/webhooks/..."
  summary_webhook: "..."       # (optional) hourly category digest — empty = off
  summary_interval: 1h         # how often to post the digest

# (Optional) AI categorization — see Step 9 for the full walkthrough
categorization:
  enabled: true                # false = disable (raw alerts still work)
  use_tweets: true             # read recent tweets as an extra signal
  tweet_count: 5               # how many recent tweets to fetch
  cache_ttl: 168h              # how long a category is cached (7 days)
  categories: [AI, Layer 2, DeFi, NFT, Meme, ...]   # base taxonomy (LLM may extend)
  openrouter:
    api_keys: ["sk-or-v1-..."] # rotated round-robin; empty = keyword-only fallback
    models: ["meta-llama/llama-3.3-70b-instruct:free", "..."]  # tried in order

# Logging
logging:
  timezone: Asia/Jakarta       # Timezone for log & alert timestamps
  level: info                  # debug | info | warn | error
```

### Tracking Modes

| Mode | `track_all_follows` | Behavior |
|------|---------------------|----------|
| **Full** | `true` | Alert on **every** new follow from your watched accounts |
| **Targeted** | `false` | Alert **only** when a watcher follows an account that's also in `twitter.txt` |

---

## 🔁 Running 24/7 (Keep It Always On)

To keep the bot running continuously on a server, pick one of these options.

### Option A — PM2 (recommended, easiest)

```bash
pm2 start ./x-tracker --name x-tracker
pm2 save
pm2 startup
```

### Option B — systemd (Linux services)

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

### Option C — screen (quick & simple)

```bash
screen -S x-tracker
./x-tracker
# Press Ctrl+A then D to detach and leave it running
```

---

## 🛠️ How It Works

```
┌──────────────┐     ┌──────────────────────┐     ┌──────────────────┐
│  twitter.txt │────▶│     X-Tracker-Bot    │────▶│  Discord (raw)   │
│  (watch list)│     │                      │     │  per-follow alert│
└──────────────┘     │  0. Refresh query IDs│     └──────────────────┘
                     │  1. Warmup baseline  │
┌──────────────┐     │  2. Scan follows     │     ┌──────────────────┐
│  X (Twitter) │◀───▶│  3. Detect new       │     │  Discord (summary)│
│  GraphQL API │     │  4. Categorize (LLM) │────▶│  hourly digest   │
└──────┬───────┘     │  5. Alert + log      │     └──────────────────┘
       │             │  6. Save state       │     ┌──────────────────┐
┌──────▼───────┐     └──────────┬───────────┘     │ state.json       │
│  OpenRouter  │◀───────────────┘                 │ events.jsonl     │
│  (LLM, free) │                                  └──────────────────┘
└──────────────┘
```

**Startup**

0. **Refresh query IDs** — Pulls X's current GraphQL query IDs from x.com's JS bundle (they rotate often), falling back to built-in values if offline.

**Main loop** (every `poll_interval`)

1. **Warmup** — On first run, fetches all current follows as a baseline (no spam).
2. **Scan** — Fetches the latest follows for each watcher (rotating the cookie pool).
3. **Detect** — Compares against the baseline to find *new* follows.
4. **Categorize** — On a cache miss, reads the target's name/bio (+ recent tweets) and asks the OpenRouter LLM for a category; result is cached for `cache_ttl`. Falls back to keyword matching when no LLM is available.
5. **Alert + log** — Sends a Discord embed (with category) and appends the follow to `events.jsonl`.
6. **Save** — Persists state to the `state/` directory.

**Summary loop** (every `summary_interval`, runs in parallel)

- Reads the last interval's events from `events.jsonl`, groups them by category, counts distinct watchers per target, and posts a digest to `summary_webhook`.

### State Files

| File | Purpose |
|------|---------|
| `state/state.json` | Baseline following list + already-sent alert pairs + category cache |
| `state/events.jsonl` | Append-only log of all detected follows (used to build the hourly summary) |

> 🔄 Want to reset? Delete the `state/` folder. The bot will re-warmup on the next start.

---

## 🧯 Troubleshooting

| Problem | Solution |
|---------|----------|
| `config invalid: no twitter cookies configured` | Fill in `auth_token` and `ct0` in `config.yaml` |
| `unauthorized (401)` | Your X cookies expired — log in again and grab fresh ones |
| `rate limited (429)` | Add more cookies to the pool, or increase `poll_interval` |
| No alerts appearing | Double-check the webhook URL and that `twitter.txt` has accounts |
| `watch_file not found` | Make sure `twitter.txt` exists in the same folder as the binary |
| Bot sends old follows on restart | Delete the `state/` folder and restart |
| `command not found: go` | Go isn't installed — revisit [Step 1](#step-1--install-go) |
| Everything shows as `Uncategorized` | No OpenRouter key set, or all models failed/hit quota — add keys/models (see [Step 9](#step-9--optional-enable-ai-categorization--hourly-summary)) |
| `openrouter ... failed: HTTP 404` | That free model name is gone — update the `models` list with a current one |
| No hourly summary appears | Set `summary_webhook`; the digest only sends when there were follows in the interval |
| `query ID refresh failed` | Harmless — the bot uses built-in fallback IDs; check network if follows also fail |

---

## ❓ Frequently Asked Questions (FAQ)

**What is X-Tracker-Bot used for?**
It tracks who specific X (Twitter) accounts follow and sends a real-time Discord alert whenever they follow someone new — ideal for alpha hunting, KOL tracking, and project discovery.

**Is X-Tracker-Bot free?**
Yes. It's free and open-source under the MIT license.

**Do I need a Twitter / X API key?**
No. The bot authenticates using your browser cookies (`auth_token` and `ct0`) instead of the official paid API.

**Is it safe to use my X account?**
Use a **dedicated alt account**, never your main. The cookies grant full account access, so keep your `config.yaml` private and never commit it to a public repo.

**How often does it check for new follows?**
By default every 10 minutes. You can change this with the `poll_interval` setting (e.g. `30s`, `5m`, `1h`).

**Will I get spammed with notifications when I first run it?**
No. The first run is a "warmup" that records existing follows silently. You'll only be alerted about follows that happen *after* startup.

**Can I track multiple accounts at once?**
Yes. Add as many accounts as you like to `twitter.txt`, one per line.

**Can I send alerts to multiple Discord channels?**
Yes. Add multiple webhook URLs under `discord.raw_webhooks`.

**Do I have to use the AI categorization?**
No. It's optional. Leave `categorization.openrouter.api_keys` empty (or set `enabled: false`) and the bot runs like the classic version. With no LLM it still does basic keyword categorization.

**Does the AI categorization cost money?**
It can be free. OpenRouter offers free-tier models (the ones ending in `:free`). The bot caches each account's category for 7 days and rotates multiple keys to stay within free limits. You can also add paid models if you prefer.

**Does fetching tweets use extra X requests?**
Yes — when `use_tweets: true`, each *newly seen* account costs one extra `UserTweets` call (drawn from the **same cookie pool** as the tracker). It only happens on a cache miss. Set `use_tweets: false` to categorize from name + bio only.

**The bot stopped finding follows after X updated — what do I do?**
Usually nothing. The bot refreshes X's GraphQL query IDs from x.com at every startup, so a simple restart picks up X's latest changes automatically.

**What operating systems does it support?**
Linux, macOS, and Windows. It's a single self-contained binary with no runtime dependencies.

**How do I avoid getting rate-limited by X?**
Add multiple cookie pairs (cookie pool) for round-robin rotation, and/or increase the `poll_interval`.

---

## 🏗️ Build from Source & Cross-Compile

```bash
# Requires Go 1.24+
git clone https://github.com/DezXBT/X-Tracker-Bot.git
cd X-Tracker-Bot
go build -ldflags="-s -w" -o x-tracker .
```

**Cross-compile for other platforms:**

```bash
# Linux ARM64 (e.g. Raspberry Pi, ARM VPS)
GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o x-tracker-linux-arm64 .

# macOS (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o x-tracker-macos .

# Windows
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o x-tracker.exe .
```

---

## 🧱 Tech Stack

- **Language:** Go (single static binary, no runtime dependencies)
- **API:** X / Twitter internal GraphQL API (cookie-based authentication, dynamic query IDs)
- **AI:** OpenRouter LLM for project categorization (free-tier capable, multi-key rotation) with keyword fallback
- **Output:** Discord webhooks (rich embeds + hourly category summary)
- **State:** JSON file persistence (baseline, dedup pairs, category cache)
- **Config:** YAML with startup validation

---

## 📄 License

Released under the **MIT License**. Free to use, modify, and distribute.

---

## 🔎 Keywords

x tracker, twitter follow tracker, twitter follow alert, x follow notification bot,
discord webhook notifier, kol tracking bot, alpha tracker, crypto twitter tracker,
x follow monitor, twitter monitoring tool, follow tracking bot, twitter bot, x bot,
follow notification, early tracking, twitter follow discord, x follow discord,
crypto twitter monitor, kol follow alert, go twitter bot, self-hosted twitter tracker
