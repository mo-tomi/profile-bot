import os
import asyncpg
import datetime

# ### 共通で使う道具（関数）は、ファイルの先頭に1回だけ書く ###
DATABASE_URL = os.environ.get('DATABASE_URL')

async def get_pool():
    if not DATABASE_URL:
        raise ValueError("DATABASE_URL environment variable is not set.")
    return await asyncpg.create_pool(DATABASE_URL)
# ###############################################################


# --- BUMPくん用の関数 ---

async def init_db():
    """BUMPくんのテーブルを初期化する"""
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


# --- 自己紹介Bot用の関数 ---

async def init_intro_bot_db():
    """自己紹介Bot専用のテーブルを作成する"""
    pool = await get_pool()
    async with pool.acquire() as connection:
        await connection.execute('''
            CREATE TABLE IF NOT EXISTS introduction_links (
                user_id BIGINT PRIMARY KEY,
                message_link TEXT NOT NULL
            );
        ''')
    await pool.close()

async def save_intro_link(user_id, message_link):
    """ユーザーの自己紹介リンクを保存または更新する"""
    pool = await get_pool()
    async with pool.acquire() as connection:
        await connection.execute('''
            INSERT INTO introduction_links (user_id, message_link) VALUES ($1, $2)
            ON CONFLICT (user_id) DO UPDATE SET message_link = $2;
        ''', user_id, message_link)
    await pool.close()

async def load_intro_link(user_id):
    """指定したユーザーの自己紹介リンクを1つだけ読み込む"""
    pool = await get_pool()
    async with pool.acquire() as connection:
        record = await connection.fetchrow("SELECT message_link FROM introduction_links WHERE user_id = $1", user_id)
    await pool.close()
    return record['message_link'] if record else None
