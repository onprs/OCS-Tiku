import json
import aiosqlite
from pathlib import Path
from typing import Optional
from datetime import datetime

from config import exe_dir


DB_PATH = exe_dir() / "ocs-tiku.db"


async def get_db() -> aiosqlite.Connection:
    db = await aiosqlite.connect(str(DB_PATH))
    db.row_factory = aiosqlite.Row
    await db.execute("PRAGMA journal_mode=WAL")
    await db.execute("""
        CREATE TABLE IF NOT EXISTS records (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            timestamp TEXT NOT NULL,
            type TEXT NOT NULL,
            status TEXT DEFAULT 'complete',
            raw_title TEXT DEFAULT '',
            raw_options TEXT DEFAULT '',
            processed_title TEXT DEFAULT '',
            processed_options TEXT DEFAULT '',
            images TEXT DEFAULT '[]',
            final_prompt TEXT DEFAULT '',
            reasoning TEXT DEFAULT '',
            raw_content TEXT DEFAULT '',
            answer TEXT DEFAULT '',
            text_model TEXT DEFAULT '',
            vision_model TEXT DEFAULT '',
            total_time_ms INTEGER DEFAULT 0
        )
    """)
    for col in ("reasoning", "status"):
        try:
            await db.execute(f"ALTER TABLE records ADD COLUMN {col} TEXT DEFAULT ''")
        except Exception:
            pass
    try:
        await db.execute("ALTER TABLE records ADD COLUMN raw_content TEXT DEFAULT ''")
    except Exception:
        pass
    await db.commit()
    return db


async def create_pending_record(question_type: str) -> int:
    db = await get_db()
    try:
        cursor = await db.execute(
            "INSERT INTO records (timestamp, type, status) VALUES (?, ?, 'pending')",
            (datetime.now().isoformat(), question_type),
        )
        await db.commit()
        return cursor.lastrowid
    finally:
        await db.close()


async def update_record(record_id: int, record: dict):
    db = await get_db()
    try:
        await db.execute(
            """UPDATE records SET
               timestamp=?, type=?, status='complete',
               raw_title=?, raw_options=?,
               processed_title=?, processed_options=?,
               images=?, final_prompt=?, reasoning=?, raw_content=?, answer=?,
               text_model=?, vision_model=?, total_time_ms=?
               WHERE id=?""",
            (
                record["timestamp"],
                record["type"],
                record.get("raw_title", ""),
                record.get("raw_options", ""),
                record.get("processed_title", ""),
                record.get("processed_options", ""),
                json.dumps(record.get("images", []), ensure_ascii=False),
                record.get("final_prompt", ""),
                record.get("reasoning", ""),
                record.get("raw_content", ""),
                record.get("answer", ""),
                record.get("text_model", ""),
                record.get("vision_model", ""),
                record.get("total_time_ms", 0),
                record_id,
            ),
        )
        await db.commit()
    finally:
        await db.close()


async def save_record(record: dict) -> int:
    db = await get_db()
    try:
        cursor = await db.execute(
            """INSERT INTO records
               (timestamp, type, status, raw_title, raw_options,
                processed_title, processed_options, images, final_prompt,
                reasoning, raw_content, answer, text_model, vision_model, total_time_ms)
               VALUES (?, ?, 'complete', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)""",
            (
                record["timestamp"],
                record["type"],
                record.get("raw_title", ""),
                record.get("raw_options", ""),
                record.get("processed_title", ""),
                record.get("processed_options", ""),
                json.dumps(record.get("images", []), ensure_ascii=False),
                record.get("final_prompt", ""),
                record.get("reasoning", ""),
                record.get("raw_content", ""),
                record.get("answer", ""),
                record.get("text_model", ""),
                record.get("vision_model", ""),
                record.get("total_time_ms", 0),
            ),
        )
        await db.commit()
        return cursor.lastrowid
    finally:
        await db.close()


async def list_records(limit: int = 50, offset: int = 0) -> list[dict]:
    db = await get_db()
    try:
        cursor = await db.execute(
            "SELECT * FROM records ORDER BY id DESC LIMIT ? OFFSET ?",
            (limit, offset),
        )
        rows = await cursor.fetchall()
        results = []
        for row in rows:
            d = dict(row)
            d["images"] = json.loads(d["images"] or "[]")
            results.append(d)
        return results
    finally:
        await db.close()


async def get_record(record_id: int) -> Optional[dict]:
    db = await get_db()
    try:
        cursor = await db.execute("SELECT * FROM records WHERE id = ?", (record_id,))
        row = await cursor.fetchone()
        if row:
            d = dict(row)
            d["images"] = json.loads(d["images"] or "[]")
            return d
        return None
    finally:
        await db.close()


async def get_stats() -> dict:
    db = await get_db()
    try:
        total = await db.execute("SELECT COUNT(*) as c FROM records WHERE status='complete'")
        total_n = (await total.fetchone())[0]

        today = await db.execute(
            "SELECT COUNT(*) as c FROM records WHERE status='complete' AND date(timestamp) = date('now', 'localtime')"
        )
        today_n = (await today.fetchone())[0]

        avg_ms = await db.execute(
            "SELECT AVG(total_time_ms) as avg FROM records WHERE status='complete'"
        )
        avg_n = (await avg_ms.fetchone())[0] or 0

        text_model = await db.execute(
            "SELECT text_model FROM records WHERE status='complete' AND text_model != '' ORDER BY id DESC LIMIT 1"
        )
        text_row = await text_model.fetchone()

        vision_model = await db.execute(
            "SELECT vision_model FROM records WHERE status='complete' AND vision_model != '' ORDER BY id DESC LIMIT 1"
        )
        vision_row = await vision_model.fetchone()

        return {
            "total": total_n,
            "today": today_n,
            "avg_time_ms": round(avg_n),
            "text_model": text_row[0] if text_row else "-",
            "vision_model": vision_row[0] if vision_row else "-",
        }
    finally:
        await db.close()
