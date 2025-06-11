import os
import asyncpg
import datetime

# ### 共通で使う道具（関数） ###
DATABASE_URL = os.environ.get('DATABASE_URL')

async def get_pool():
    if not DATABASE_URL:
        raise ValueError("DATABASE_URL environment variable is not set.")
    return await asyncpg.create_pool(DATABASE_URL)
# #################################


# --- BUMPくん用の関数 ---
async def init_db():
    pool = await get_pool()
    async with pool.acquire() as connection:
        await connection.execute('''
            CREATE TABLE IF NOT EXISTS users (
                user_id BIGINT PRIMARY KEY,
                bump_count INTEGER NOT NULL DEFAULT 0
            );
        ''')
        await connection.execute('''
            CREATE TABLE IF NOT EXISTS reminders (
                id SERIAL PRIMARY KEY,
                channel_id BIGINT NOT NULL,
                remind_at TIMESTAMP WITH TIME ZONE NOT NULL
            );
        ''')
        await connection.execute('''
            CREATE TABLE IF NOT EXISTS settings (
                key TEXT PRIMARY KEY,
                value TEXT
            );
        ''')
        await connection.execute('''
            INSERT INTO settings (key, value) VALUES ('scan_completed', 'false')
            ON CONFLICT (key) DO NOTHING;
        ''')
    await pool.close()

async def is_scan_completed():
    pool = await get_pool()
    async with pool.acquire() as connection:
        record = await connection.fetchrow("SELECT value FROM settings WHERE key = 'scan_completed'")
    await pool.close()
    return record and record['value'] == 'true'

async def mark_scan_as_completed():
    pool = await get_pool()
    async with pool.acquire() as connection:
        await connection.execute("UPDATE settings SET value = 'true' WHERE key = 'scan_completed'")
    await pool.close()

async def record_bump(user_id):
    pool = await get_pool()
    async with pool.acquire() as connection:
        await connection.execute('''
            INSERT INTO users (user_id, bump_count) VALUES ($1, 1)
            ON CONFLICT (user_id) DO UPDATE SET bump_count = users.bump_count + 1;
        ''', user_id)
        count = await connection.fetchval('SELECT bump_count FROM users WHERE user_id = $1', user_id)
    await pool.close()
    return count

async def get_top_users():
    pool = await get_pool()
    async with pool.acquire() as connection:
        records = await connection.fetch('SELECT user_id, bump_count FROM users ORDER BY bump_count DESC LIMIT 5')
    await pool.close()
    return records

async def get_user_count(user_id):
    pool = await get_pool()
    async with pool.acquire() as connection:
        count = await connection.fetchval('SELECT bump_count FROM users WHERE user_id = $1', user_id)
    await pool.close()
    return count or 0

async def set_reminder(channel_id, remind_time):
    pool = await get_pool()
    async with pool.acquire() as connection:
        await connection.execute('DELETE FROM reminders')
        await connection.execute('INSERT INTO reminders (channel_id, remind_at) VALUES ($1, $2)', channel_id, remind_time)
    await pool.close()

async def get_reminder():
    pool = await get_pool()
    async with pool.acquire() as connection:
        record = await connection.fetchrow('SELECT channel_id, remind_at FROM reminders ORDER BY remind_at LIMIT 1')
    await pool.close()
    return record

async def clear_reminder():
    pool = await get_pool()
    async with pool.acquire() as connection:
        await connection.execute('DELETE FROM reminders')
    await pool.close()

async def get_total_bumps():
    pool = await get_pool()
    async with pool.acquire() as connection:
        total = await connection.fetchval('SELECT SUM(bump_count) FROM users')
    await pool.close()
    return total or 0


# --- 自己紹介Bot用の関数 (v2仕様) ---
async def init_intro_bot_db():
    """自己紹介Bot専用のテーブルを作成する"""
    pool = await get_pool()
    async with pool.acquire() as connection:
        # メッセージリンクの代わりに、チャンネルIDとメッセージIDを保存する
        await connection.execute('''
            CREATE TABLE IF NOT EXISTS introductions (
                user_id BIGINT PRIMARY KEY,
                channel_id BIGINT NOT NULL,
                message_id BIGINT NOT NULL
            );
        ''')
    await pool.close()

async def save_intro(user_id, channel_id, message_id):
    """ユーザーの自己紹介IDを保存または更新する"""
    pool = await get_pool()
    async with pool.acquire() as connection:
        await connection.execute('''
            INSERT INTO introductions (user_id, channel_id, message_id) VALUES ($1, $2, $3)
            ON CONFLICT (user_id) DO UPDATE SET channel_id = $2, message_id = $3;
        ''', user_id, channel_id, message_id)
    await pool.close()

async def get_intro_ids(user_id):
    """指定したユーザーの自己紹介IDセットを取得する"""
    pool = await get_pool()
    async with pool.acquire() as connection:
        # channel_id と message_id の両方を返す
        record = await connection.fetchrow(
            "SELECT channel_id, message_id FROM introductions WHERE user_id = $1", user_id
        )
    await pool.close()
    return record # 存在しない場合はNoneが返る


# --- 守護神ボット用の関数 ---
async def init_shugoshin_db():
    pool = await get_pool()
    async with pool.acquire() as connection:
        await connection.execute('''
            CREATE TABLE IF NOT EXISTS reports (
                report_id SERIAL PRIMARY KEY, guild_id BIGINT, message_id BIGINT,
                target_user_id BIGINT, violated_rule TEXT, details TEXT,
                message_link TEXT, urgency TEXT, status TEXT DEFAULT '未対応',
                created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
            );
        ''')
        await connection.execute('''
            CREATE TABLE IF NOT EXISTS guild_settings (
                guild_id BIGINT PRIMARY KEY,
                report_channel_id BIGINT,
                urgent_role_id BIGINT
            );
        ''')
        await connection.execute('''
            CREATE TABLE IF NOT EXISTS report_cooldowns (
                user_id BIGINT PRIMARY KEY,
                last_report_at TIMESTAMP WITH TIME ZONE NOT NULL
            );
        ''')
    await pool.close()

async def setup_guild(guild_id, report_channel_id, urgent_role_id):
    pool = await get_pool()
    async with pool.acquire() as connection:
        await connection.execute('''
            INSERT INTO guild_settings (guild_id, report_channel_id, urgent_role_id)
            VALUES ($1, $2, $3)
            ON CONFLICT (guild_id) DO UPDATE
            SET report_channel_id = $2, urgent_role_id = $3;
        ''', guild_id, report_channel_id, urgent_role_id)
    await pool.close()

async def get_guild_settings(guild_id):
    pool = await get_pool()
    async with pool.acquire() as connection:
        settings = await connection.fetchrow(
            "SELECT report_channel_id, urgent_role_id FROM guild_settings WHERE guild_id = $1",
            guild_id
        )
    await pool.close()
    return settings

async def check_cooldown(user_id, cooldown_seconds):
    pool = await get_pool()
    async with pool.acquire() as connection:
        async with connection.transaction():
            record = await connection.fetchrow(
                "SELECT last_report_at FROM report_cooldowns WHERE user_id = $1", user_id
            )
            now = datetime.datetime.now(datetime.timezone.utc)
            if record:
                time_since_last = now - record['last_report_at']
                if time_since_last.total_seconds() < cooldown_seconds:
                    return cooldown_seconds - time_since_last.total_seconds()
            await connection.execute('''
                INSERT INTO report_cooldowns (user_id, last_report_at) VALUES ($1, $2)
                ON CONFLICT (user_id) DO UPDATE SET last_report_at = $2;
            ''', user_id, now)
            return 0
    await pool.close()

async def create_report(guild_id, target_user_id, violated_rule, details, message_link, urgency):
    pool = await get_pool()
    async with pool.acquire() as connection:
        report_id = await connection.fetchval(
            '''INSERT INTO reports (guild_id, target_user_id, violated_rule, details, message_link, urgency) 
               VALUES ($1, $2, $3, $4, $5, $6) RETURNING report_id''',
            guild_id, target_user_id, violated_rule, details, message_link, urgency
        )
    await pool.close()
    return report_id

async def update_report_message_id(report_id, message_id):
    pool = await get_pool()
    async with pool.acquire() as connection:
        await connection.execute(
            "UPDATE reports SET message_id = $1 WHERE report_id = $2",
            message_id, report_id
        )
    await pool.close()

async def update_report_status(report_id, new_status):
    pool = await get_pool()
    async with pool.acquire() as connection:
        await connection.execute(
            "UPDATE reports SET status = $1 WHERE report_id = $2",
            new_status, report_id
        )
    await pool.close()

async def get_report(report_id):
    pool = await get_pool()
    async with pool.acquire() as connection:
        record = await connection.fetchrow("SELECT * FROM reports WHERE report_id = $1", report_id)
    await pool.close()
    return record

async def list_reports(status_filter=None):
    pool = await get_pool()
    query = "SELECT report_id, target_user_id, status FROM reports"
    params = []
    if status_filter and status_filter != 'all':
        query += " WHERE status = $1"
        params.append(status_filter)
    query += " ORDER BY report_id DESC LIMIT 20"
    async with pool.acquire() as connection:
        records = await connection.fetch(query, *params)
    await pool.close()
    return records

async def get_report_stats():
    pool = await get_pool()
    async with pool.acquire() as connection:
        stats = await connection.fetch('''
            SELECT status, COUNT(*) as count 
            FROM reports 
            GROUP BY status
        ''')
    await pool.close()
    return {row['status']: row['count'] for row in stats}
