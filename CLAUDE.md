# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## ⚠️ IMPORTANT: Required Reading Before Any Task

**Before starting ANY work in this repository, you MUST read the full contents of ALL markdown files in the `docs/` directory:**

1. `docs/# Discord自己紹介Bot Go移行プロジェクト 要件定義書.md` - Project requirements and specifications
2. `docs/claude-code-quickstart.md` - Quick start guide
3. `docs/technical-reference.md` - Technical reference documentation
4. `docs/claude-code-prompt.md` - Claude Code specific prompts and guidelines

**Do not skip this step.** These documents contain critical context, requirements, and guidelines that must inform all development work.

## Overview

This is a Discord bot built with py-cord that manages self-introductions and daily reminders for server members. The bot monitors voice channel activity and automatically posts members' self-introductions when they join specific voice channels. It also includes a daily reminder system to encourage members who haven't posted introductions yet.

## Running the Bot

**Start the bot:**
```bash
python main.py
```

The bot requires these environment variables:
- `TOKEN` - Discord bot token
- `DATABASE_URL` - PostgreSQL connection string (asyncpg format)
- `PORT` - (Optional) Web server port, defaults to 8080

**Docker:**
```bash
docker build -t profile-bot .
docker run -e TOKEN=your_token -e DATABASE_URL=your_db_url profile-bot
```

## Architecture

### Core Components

**main.py** - Primary bot logic with three main systems:
1. **Voice Channel Monitoring** (`on_voice_state_update`): Detects when members join specific voice channels and posts their self-introduction to a notification channel
2. **Introduction Tracking** (`on_message`): Captures self-introduction messages posted in the designated introduction channel
3. **Daily Reminder System** (`daily_reminder_task`): Sends automated reminders at 10:00 AM daily to members without introductions

**database.py** - Database abstraction layer using asyncpg with connection pooling. Contains functions for multiple bot features (BUMP bot, shugoshin bot, introduction bot, daily reminders). The introduction-related functions are:
- `init_intro_bot_db()` - Initialize introduction tables
- `save_intro()` / `get_intro_ids()` - Store and retrieve introduction message references
- `get_members_without_intro()` - Find members who haven't posted introductions
- `init_daily_reminder_db()` / `check_daily_reminder_sent()` / `log_daily_reminder()` - Daily reminder tracking

**keep_alive.py** - Simple Flask web server for health checks and uptime monitoring

### Database Schema

**introductions table:**
- `user_id` (BIGINT, PK) - Discord user ID
- `channel_id` (BIGINT) - Channel where introduction was posted
- `message_id` (BIGINT) - Message ID of the introduction
- `created_at` (TIMESTAMP) - When the introduction was created/updated

**daily_reminder_log table:**
- `id` (SERIAL, PK)
- `reminder_date` (DATE) - Date the reminder was sent
- `notified_users` (TEXT[]) - Array of user IDs who were notified
- `created_at` (TIMESTAMP)

### Key Configuration

**Channel IDs** (hardcoded in main.py:23-24):
- `INTRODUCTION_CHANNEL_ID = 1300659373227638794`
- `NOTIFICATION_CHANNEL_ID = 1331177944244289598`

**Monitored Voice Channels** (main.py:27-31):
List of 8 voice channel IDs that trigger introduction notifications

**Excluded Bot IDs** (main.py:243):
Bots that won't trigger introduction notifications: `[533698325203910668, 916300992612540467, 1300226846599675974]`

### Display Name Resolution

The bot has sophisticated display name handling (main.py:72-145):
- `get_member_display_name_fast()` - Quick lookup without API calls
- `resolve_member_display_name()` - Multi-level fallback system that tries: guild nick → global_name → display_name → API fetch member → API fetch user

This ensures the bot shows the most accurate "server display name" even with Discord's new global display name system.

### Startup Behavior

On bot startup (`on_ready` in main.py:148-229):
1. Initializes database tables for introductions and daily reminders
2. Scans the introduction channel history (up to 3000 messages)
3. Saves all found introductions to the database
4. Starts the daily reminder background task
5. Logs detailed statistics about the scan process

### Daily Reminder Logic

- Runs at 10:00 AM daily via `daily_reminder_task()`
- Checks if reminder was already sent today (prevents duplicates)
- Mentions up to 10 members by name (displays "ほか N名" if more)
- Uses `resolve_member_display_name()` for accurate name display
- Logs all notified user IDs to prevent duplicate notifications

### Slash Commands

`/profilebot` - Manually triggers the introduction reminder (force mode, bypasses daily check)

## Development Notes

### Connection Pooling

The database module uses asyncpg with `statement_cache_size=0` for pgbouncer compatibility (database.py:28). The pool is managed globally and reused across all database operations.

### Error Handling Philosophy

The bot implements extensive fallback logic:
- If introduction message is deleted, sends notification without embed
- If database lookup fails, still sends basic join notification
- All errors are logged with emoji prefixes for easy scanning (✅/❌/⚠️)

### Logging

Uses Python's standard logging with emoji prefixes:
- 🔊 Voice channel events
- 📝 Database saves
- 🔍 Search operations
- ✅ Successful operations
- ❌ Errors
- ⚠️ Warnings

### Signal Handling

Implements graceful shutdown on SIGTERM/SIGINT (main.py:54-70):
- Closes database pool
- Closes bot connection
- Logs shutdown process

---

## 🚀 Go Migration Project (In Progress)

### Migration Overview

**Status**: This project is being migrated from Python (py-cord) to Go (discordgo).

**Critical Problem**: K8s環境で複数レプリカ実行時にリマインダーが重複送信される
**Primary Goal**: PostgreSQL Advisory Lockによる排他制御で重複を完全防止
**Secondary Benefits**:
- Memory reduction: 75% (200MB → 50MB)
- Startup time reduction: 70% (15秒 → 5秒)

### Architecture Migration Path

```
FROM: Python + py-cord + Flask + asyncpg
TO:   Go + discordgo + pgx + net/http
```

### Migration Rules

**IMPORTANT: Python Code Archival**
- DO NOT delete existing Python files
- Move them to `archive/python/` directory using `git mv`
- Preserve all Python code for reference and rollback capability

```bash
mkdir -p archive/python
git mv *.py archive/python/
git mv requirements.txt archive/python/
git mv Dockerfile archive/python/Dockerfile.old
```

### Go Project Structure

```
profile-bot/
├── cmd/bot/main.go                    # Entry point
├── internal/
│   ├── bot/
│   │   ├── bot.go                    # Bot initialization
│   │   ├── handlers.go               # Event handlers
│   │   ├── commands.go               # Slash commands
│   │   └── reminder.go               # Reminder with Advisory Lock
│   ├── database/
│   │   ├── db.go                     # Connection pooling
│   │   ├── intro.go                  # Introduction CRUD
│   │   ├── reminder.go               # Reminder logs
│   │   └── lock.go                   # PostgreSQL Advisory Lock
│   ├── config/
│   │   ├── config.go                 # Configuration loader
│   │   └── roles.go                  # Role management
│   └── utils/
│       ├── logger.go                 # Structured logging
│       └── embed.go                  # Discord embed builder
├── configs/roles.yaml                # Role categories config
└── deployments/
    ├── docker/Dockerfile
    └── k8s/
        ├── deployment.yaml           # Multi-replica deployment
        ├── service.yaml
        └── configmap.yaml
```

### Critical Implementation Requirements

#### 1. PostgreSQL Advisory Lock (HIGHEST PRIORITY)

**Purpose**: Prevent duplicate reminder execution in multi-replica K8s environment

**Implementation Pattern**:
```go
// Acquire lock before executing weekly reminder
acquired, err := AcquireAdvisoryLock(ctx, pool, "weekly_reminder")
if !acquired {
    logger.Info("Another pod is executing, skipping")
    return
}
defer ReleaseAdvisoryLock(ctx, pool, "weekly_reminder")
```

**Key Points**:
- Use `pg_try_advisory_lock(hashtext('weekly_reminder'))` for distributed locking
- Always release lock using `defer` to guarantee cleanup
- If lock acquisition fails, skip execution (another pod is running)
- Combine with daily execution log check for double protection

#### 2. VC Text Chat (Text-in-Voice) Integration

**Requirements**:
- Post introduction to Voice Channel's dedicated text chat (Text-in-Voice feature)
- Silently skip if text chat is unavailable (NO error notification)
- Include role information formatted by category
- Display introduction content with link button

**Error Handling**:
```go
textChannelID, err := getVoiceChannelTextChat(session, voiceChannelID)
if err != nil {
    // Silent skip - no error notification
    logger.Debug("VC text chat unavailable", "vc", voiceChannelID)
    return
}
```

#### 3. Role Display Categories

Must read from `configs/roles.yaml` and format by category:
- 障害 (Disabilities)
- 性別 (Gender)
- 手帳 (Support documents)
- コミュニケーション (Communication preferences)

**Filtering Rules**:
- Exclude: "@everyone", "Carl-bot", bot-managed roles, roles ending with "bot"/"Bot"
- Special handling: Remove "（聴覚者）" suffix automatically

#### 4. Weekly Reminder Schedule

**Timing**: Every Monday 10:00 JST
**Technology**: `robfig/cron/v3` with JST timezone
**Must Include**:
- Advisory Lock for exclusive execution
- Daily log check (prevent same-day duplicates)
- Graceful handling when all members have introductions

### Migration Phases

**Phase 1**: Project setup & database layer (1-2 days)
- Initialize Go modules with required dependencies
- Implement database connection pooling (pgx)
- **Implement Advisory Lock functions** (critical)

**Phase 2**: Core feature implementation (3-4 days)
- Introduction channel monitoring with auto-scan (3000 messages)
- VC entry notification with Text-in-Voice posting
- Weekly reminder with Advisory Lock
- Role display from YAML config

**Phase 3**: K8s readiness (1-2 days)
- Multi-stage Dockerfile
- Health check endpoints (`/health`, `/ready`)
- Graceful shutdown handling
- K8s manifests with 2+ replicas

**Phase 4**: Testing & verification (1-2 days)
- Test Advisory Lock prevents duplicates with multiple pods
- Verify VC text chat posting works correctly
- Confirm role categorization displays properly

### Database Schema (Unchanged)

**Keep existing tables**:
- `introductions` - No schema changes
- `daily_reminder_log` - Already exists in Python version

**Same environment variables**:
- `DISCORD_TOKEN` (renamed from `TOKEN` for clarity)
- `DATABASE_URL` (PostgreSQL connection string)

### Key Dependencies

```go
github.com/bwmarrin/discordgo      // Discord API
github.com/jackc/pgx/v5/pgxpool    // PostgreSQL with pooling
github.com/robfig/cron/v3          // Cron scheduling
gopkg.in/yaml.v3                   // YAML config parsing
```

### Testing Checklist (Multi-Replica Environment)

- [ ] Advisory Lock prevents duplicate reminders with 2+ pods
- [ ] Introduction auto-saves on new messages
- [ ] Role auto-assignment works ("自己紹介済み" role)
- [ ] VC entry triggers text chat posting
- [ ] Role categories display correctly
- [ ] `/profilebot` slash command executes manually
- [ ] Health check endpoints respond
- [ ] Graceful shutdown closes DB connections cleanly

### Reference Documents

All Go migration details are documented in:
- `docs/claude-code-prompt.md` - Complete implementation guide
- `docs/technical-reference.md` - Technical specifications
- `docs/# Discord自己紹介Bot Go移行プロジェクト 要件定義書.md` - Requirements document

**Read these documents in full before starting any Go implementation work.**
