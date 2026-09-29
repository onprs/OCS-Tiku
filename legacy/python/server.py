import json
import asyncio
import mimetypes
from pathlib import Path
from contextlib import asynccontextmanager

# 修复 Windows 注册表导致 MIME 类型错误的问题，防止前端白屏
mimetypes.add_type('application/javascript', '.js')
mimetypes.add_type('text/css', '.css')
mimetypes.add_type('image/svg+xml', '.svg')

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse, Response, HTMLResponse
from fastapi.staticfiles import StaticFiles
from fastapi.middleware.cors import CORSMiddleware

from config import config, bundle_dir, exe_dir
from solver import solver
from store import save_record, create_pending_record, update_record
from dashboard import router as dashboard_router


@asynccontextmanager
async def lifespan(app: FastAPI):
    yield


app = FastAPI(
    title="OCS 超星题库",
    version="1.0.0",
    lifespan=lifespan,
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

frontend_dist = bundle_dir() / "frontend" / "dist"
assets_dir = frontend_dist / "assets"
if assets_dir.exists():
    app.mount("/assets", StaticFiles(directory=str(assets_dir)), name="assets")

app.include_router(dashboard_router)

images_dir = exe_dir() / "images"
images_dir.mkdir(parents=True, exist_ok=True)
app.mount("/images", StaticFiles(directory=str(images_dir)), name="images")


@app.api_route("/{path:path}", methods=["HEAD"])
async def head_handler(path: str):
    return Response(status_code=200)


@app.post("/api/search")
async def search(request: Request):
    try:
        body = await request.json()
    except Exception:
        return JSONResponse({"error": "Invalid JSON"}, status_code=400)

    title = body.get("title", "")
    options = body.get("options", "")
    question_type = body.get("type", "single")

    record_id = await create_pending_record(question_type)

    async def on_complete(record):
        await update_record(record_id, record)

    try:
        result = await solver.solve(title, options, question_type, record_callback=on_complete)
    except Exception as e:
        await update_record(record_id, {
            "timestamp": str(__import__("datetime").datetime.now().isoformat()),
            "type": question_type,
            "answer": f"ERROR: {e}",
            "total_time_ms": 0,
        })
        return JSONResponse({"error": str(e), "question": title, "answer": ""}, status_code=500)

    return JSONResponse({
        "question": result["processed_title"],
        "answer": result["answer"],
    })


@app.get("/{full_path:path}", response_class=HTMLResponse)
async def catch_all(full_path: str):
    # 如果请求的是明确的 public 文件（带后缀且文件存在），则直接返回该文件
    target_file = frontend_dist / full_path
    if full_path and target_file.exists() and target_file.is_file():
        mime_type, _ = mimetypes.guess_type(target_file)
        return Response(content=target_file.read_bytes(), media_type=mime_type or "application/octet-stream")
        
    index_file = frontend_dist / "index.html"
    if index_file.exists():
        # 禁止缓存 index.html，防止前端发版后页面白屏
        return Response(
            content=index_file.read_bytes(),
            media_type="text/html; charset=utf-8",
            headers={
                "Cache-Control": "no-cache, no-store, must-revalidate",
                "Pragma": "no-cache",
                "Expires": "0",
            }
        )
        
    return "Frontend not built yet. Please run 'npm run build' in frontend directory."
