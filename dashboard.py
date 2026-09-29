import json

from fastapi import APIRouter, Request, HTTPException
from fastapi.responses import JSONResponse

from config import config, bundle_dir
from store import list_records, get_record, get_stats, get_daily_counts, count_all_records
from solver import solver

router = APIRouter()

def build_ocs_config(server_host: str, server_port: int) -> list:
    return [{
        "name": "OCS超星LLM题库",
        "url": f"http://{server_host}:{server_port}/api/search",
        "homepage": f"http://{server_host}:{server_port}",
        "method": "post",
        "contentType": "json",
        "type": "GM_xmlhttpRequest",
        "headers": {"Content-Type": "application/json"},
        "data": {
            "title": "${title}",
            "options": "${options}",
            "type": "${type}",
        },
        "handler": "return (res)=>[res.question, res.answer]",
    }]


@router.get("/api/records")
async def api_records(page: int = 1, limit: int = 50):
    offset = (page - 1) * limit
    records = await list_records(limit=limit, offset=offset)
    total = await count_all_records()
    pages = (total + limit - 1) // limit if total > 0 else 1
    return {
        "records": records,
        "total": total,
        "page": page,
        "pages": pages,
    }


@router.get("/api/records/{record_id}")
async def api_record_detail(record_id: int):
    record = await get_record(record_id)
    if not record:
        raise HTTPException(status_code=404, detail="记录不存在")
    return record


@router.get("/api/settings")
async def api_settings_get():
    text_cfg = config.get_text_config()
    vision_cfg = config.get_vision_config()
    answer_cfg = config.answer_config
    return {
        "text": text_cfg,
        "vision": vision_cfg,
        "answer": answer_cfg,
        "text_providers": solver.available_text_providers(),
        "vision_providers": solver.available_vision_providers(),
    }


@router.put("/api/settings/text")
async def save_text_settings(data: dict):
    config.set_text_config(**data)
    config.save()
    solver.reload_models()
    return {"ok": True}


@router.put("/api/settings/vision")
async def save_vision_settings(data: dict):
    config.set_vision_config(**data)
    config.save()
    solver.reload_models()
    return {"ok": True}


@router.put("/api/settings/answer")
async def save_answer_settings(data: dict):
    config._data["answer"] = data
    config.save()
    return {"ok": True}


@router.get("/api/test-text")
async def test_text():
    try:
        ok = await solver.test_text()
        return {"ok": ok}
    except Exception as e:
        return JSONResponse({"ok": False, "error": str(e)}, status_code=400)


@router.get("/api/test-vision")
async def test_vision():
    try:
        ok = await solver.test_vision()
        return {"ok": ok}
    except Exception as e:
        return JSONResponse({"ok": False, "error": str(e)}, status_code=400)


@router.get("/api/stats")
async def api_stats():
    return await get_stats()


@router.get("/api/config")
async def api_config():
    ocs_config = build_ocs_config(config.server_host, config.server_port)
    return ocs_config


@router.get("/api/dashboard")
async def api_dashboard():
    stats = await get_stats()
    records = await list_records(limit=20)
    daily_counts = await get_daily_counts(7)
    return {"stats": stats, "records": records, "daily_counts": daily_counts}


@router.get("/api/records/recent")
async def api_recent_records(since: int = 0, limit: int = 20):
    records = await list_records(limit=limit)
    if since > 0:
        records = [r for r in records if r["id"] > since]
    return records
