import re
import asyncio
import hashlib
from io import BytesIO
from pathlib import Path
from typing import List, Tuple
from urllib.parse import urlparse
from config import exe_dir

import httpx
from PIL import Image

IMAGE_URL_PATTERN = re.compile(r"https?://[^\s]+", re.IGNORECASE)
MAX_IMAGE_SIZE_MB = 5
MAX_RETRIES = 5

REFERER_MAP = {
    "chaoxing.com": "https://mooc1.chaoxing.com/",
    "xueyinonline.com": "https://mooc1.chaoxing.com/",
    "hnsyu.net": "https://mooc1.chaoxing.com/",
    "zhihuishu.com": "https://onlineweb.zhihuishu.com/",
    "icve.com.cn": "https://zjy2.icve.com.cn/",
    "icourse163.org": "https://www.icourse163.org/",
    "yuketang.cn": "https://www.yuketang.cn/",
}

UA_VARIANTS = [
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36",
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0",
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36",
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:126.0) Gecko/20100101 Firefox/126.0",
]


def _build_headers(url: str, attempt: int) -> dict:
    parsed = urlparse(url)
    hostname = parsed.hostname or ""
    referer = None
    for domain, ref in REFERER_MAP.items():
        if hostname.endswith(domain):
            referer = ref
            break
    if referer is None:
        parts = hostname.rsplit(".", 2)
        referer = f"https://{'.'.join(parts[-2:])}/" if len(parts) >= 2 else f"https://{hostname}/"

    headers = {
        "Referer": referer,
        "User-Agent": UA_VARIANTS[attempt % len(UA_VARIANTS)],
        "Accept": "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8",
        "Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
    }
    if attempt >= 3:
        headers.pop("Referer", None)
    return headers


def extract_image_urls(text: str) -> List[str]:
    return IMAGE_URL_PATTERN.findall(text)


def replace_image_urls(text: str, replacements: dict) -> str:
    result = text
    for url, desc in replacements.items():
        result = result.replace(url, f"[图片: {desc}]")
    return result


async def download_image(client: httpx.AsyncClient, url: str, attempt: int = 0) -> bytes:
    headers = _build_headers(url, attempt)
    resp = await client.get(url, headers=headers, follow_redirects=True, timeout=25)
    if resp.status_code == 404:
        resp.raise_for_status()
    content_type = resp.headers.get("content-type", "")
    if not content_type.startswith("image/") and "octet-stream" not in content_type:
        resp.raise_for_status()
        raise ValueError(f"Not an image: {content_type}")
    resp.raise_for_status()

    data = resp.content
    size_mb = len(data) / (1024 * 1024)
    if size_mb > MAX_IMAGE_SIZE_MB:
        img = Image.open(BytesIO(data))
        img.thumbnail((1024, 1024), Image.LANCZOS)
        buf = BytesIO()
        img_format = img.format or "PNG"
        img.save(buf, format=img_format)
        data = buf.getvalue()

    return data


def _classify_failure(error: str) -> str:
    if "404" in error:
        return "(图片已失效)"
    if "403" in error:
        return "(图片拒绝访问)"
    if "空白" in error or "empty" in error.lower():
        return "(空白)"
    return "(未识别)"


def _normalize_urls(url: str) -> list:
    urls = [url]
    parsed = urlparse(url)
    parts = parsed.path.rstrip("/").split("/")
    while len(parts) > 1 and len(parts[-1]) <= 2:
        parts.pop()
        new_path = "/".join(parts)
        new_url = parsed._replace(path=new_path).geturl()
        urls.append(new_url)
    return urls


def _get_images_dir() -> Path:
    img_dir = exe_dir() / "images"
    img_dir.mkdir(parents=True, exist_ok=True)
    return img_dir


def _save_image(url: str, data: bytes) -> str:
    img_dir = _get_images_dir()
    url_hash = hashlib.md5(url.encode()).hexdigest()[:12]
    img = Image.open(BytesIO(data))
    ext = (img.format or "PNG").lower()
    if ext == "jpeg":
        ext = "jpg"
    filename = f"{url_hash}.{ext}"
    filepath = img_dir / filename
    if not filepath.exists():
        img.save(filepath, format=ext.upper() if ext != "jpg" else "JPEG")
    return f"/images/{filename}"


async def preprocess_text(
    title: str,
    options: str,
    vision_model,
    vision_prompt: str = (
        "请精确识别图片中的内容。如果是数学公式，用LaTeX格式完整输出每个符号，例如：\\frac{1}{2}、\\sqrt{x}、\\int_{a}^{b}。"
        "如果是图表或文字，描述关键信息。如果图片空白或完全无法辨认，回复'空白'。只输出识别结果，不解释。"
    ),
) -> Tuple[str, str, List[dict]]:
    all_images = extract_image_urls(title) + extract_image_urls(options)
    img_infos: List[dict] = []

    if not all_images:
        return title, options, img_infos

    if vision_model is None:
        for url in all_images:
            img_infos.append({"url": url, "description": "(未配置视觉模型)", "status": "failed"})
        return title, options, img_infos

    replacements = {}
    urls_dedup = list(dict.fromkeys(all_images))

    async def process_one(url: str) -> Tuple[str, str, str]:
        last_error = ""
        urls_to_try = _normalize_urls(url)
        local_path = ""
        for norm_url in urls_to_try:
            for attempt in range(MAX_RETRIES):
                try:
                    async with httpx.AsyncClient(verify=True, timeout=30) as client:
                        img_bytes = await download_image(client, norm_url, attempt)
                    local_path = _save_image(url, img_bytes)
                    desc = await vision_model.describe(img_bytes, vision_prompt)
                    if desc.strip():
                        return url, desc.strip(), local_path
                    last_error = "VL returned empty"
                    break
                except Exception as e:
                    last_error = str(e).split("\n")[0][:120]
                    if "404" in last_error:
                        break
                    if "502" in last_error or "503" in last_error or "504" in last_error or "timed out" in last_error.lower():
                        await asyncio.sleep(1.5 * (2 ** min(attempt, 3)))
                    elif attempt < MAX_RETRIES - 1:
                        await asyncio.sleep(1.0 * (2 ** min(attempt, 3)))
        return url, _classify_failure(last_error), local_path

    results = await asyncio.gather(*[process_one(url) for url in urls_dedup])

    for url, desc, local_path in results:
        replacements[url] = desc
        img_infos.append({
            "url": url,
            "description": desc,
            "src": local_path,
            "status": "done" if "识别失败" not in desc and "未识别" not in desc and "图片已失效" not in desc and "图片拒绝访问" not in desc and "空白" not in desc else "failed",
        })

    title = replace_image_urls(title, replacements)
    options = replace_image_urls(options, replacements)

    return title, options, img_infos
