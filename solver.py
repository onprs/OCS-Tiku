import re
import base64
from typing import Optional
from datetime import datetime

from config import config
from models.registry import get_text_provider, get_vision_provider, list_text_providers, list_vision_providers
from models.base import TextModel, VisionModel

OPTION_LETTERS = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
ANSWER_TAG = re.compile(r"<answer>\s*(.*?)\s*</answer>", re.DOTALL | re.IGNORECASE)


class Solver:
    def __init__(self):
        self._text_model: Optional[TextModel] = None
        self._vision_model: Optional[VisionModel] = None
        self.reload_models()

    def reload_models(self):
        text_cfg = config.get_text_config()
        text_provider_name = text_cfg.pop("provider", "deepseek")
        if text_provider_name:
            text_cls = get_text_provider(text_provider_name)
            self._text_model = text_cls(**text_cfg)
        else:
            self._text_model = None

        vision_cfg = config.get_vision_config()
        vision_provider_name = vision_cfg.pop("provider", "")
        if vision_provider_name:
            vision_cls = get_vision_provider(vision_provider_name)
            self._vision_model = vision_cls(**vision_cfg)
        else:
            self._vision_model = None

    @property
    def text_model(self) -> TextModel:
        return self._text_model

    @property
    def vision_model(self) -> VisionModel:
        return self._vision_model

    async def solve(self, title, options, question_type, record_callback=None) -> dict:
        answer_cfg = config.answer_config
        mode = answer_cfg.get("mode", "stepwise")
        if mode == "direct" and self.vision_model and hasattr(self.vision_model, "direct_answer"):
            return await self._solve_direct(title, options, question_type, record_callback)
        return await self._solve_stepwise(title, options, question_type, record_callback)

    async def _solve_stepwise(self, title, options, question_type, record_callback) -> dict:
        from preprocessor import preprocess_text

        start_time = datetime.now()
        answer_cfg = config.answer_config
        system_prompt = answer_cfg.get("system_prompt", "")

        processed_title, processed_options, img_infos = await preprocess_text(title, options, self.vision_model)
        labelled_options, _ = self._label_options(processed_options)
        prompt = self._build_prompt(processed_title, labelled_options, question_type)

        content, reasoning = await self.text_model.chat(system_prompt, prompt)
        answer = self._extract_tag(content, reasoning)

        elapsed = (datetime.now() - start_time).total_seconds()
        record = {
            "timestamp": start_time.isoformat(), "type": question_type,
            "raw_title": title, "raw_options": options,
            "processed_title": processed_title, "processed_options": processed_options,
            "images": img_infos, "final_prompt": prompt,
            "reasoning": reasoning or "", "raw_content": content or "", "answer": answer,
            "text_model": self.text_model.display_name if self.text_model else "none",
            "vision_model": self.vision_model.display_name if self.vision_model else "none",
            "total_time_ms": round(elapsed * 1000),
        }
        if record_callback:
            await record_callback(record)
        return record

    async def _solve_direct(self, title, options, question_type, record_callback) -> dict:
        from preprocessor import extract_image_urls

        start_time = datetime.now()
        answer_cfg = config.answer_config
        system_prompt = answer_cfg.get("system_prompt", "")

        all_urls = extract_image_urls(title) + extract_image_urls(options)
        img_infos = []
        url_images = {}
        if all_urls and self.vision_model:
            url_images, img_infos = await self._download_for_direct(all_urls)

        messages = self._build_direct_messages(title, options, question_type, url_images)
        content, reasoning = await self.vision_model.direct_answer(system_prompt, messages)
        answer = self._extract_tag(content, reasoning)

        prompt_preview = self._build_direct_prompt_preview(title, options, question_type, url_images)
        elapsed = (datetime.now() - start_time).total_seconds()
        record = {
            "timestamp": start_time.isoformat(), "type": question_type,
            "raw_title": title, "raw_options": options,
            "processed_title": title, "processed_options": options,
            "images": img_infos, "final_prompt": prompt_preview,
            "reasoning": reasoning or "", "raw_content": content or "", "answer": answer,
            "text_model": self.vision_model.display_name + " (direct)",
            "vision_model": self.vision_model.display_name if self.vision_model else "none",
            "total_time_ms": round(elapsed * 1000),
        }
        if record_callback:
            await record_callback(record)
        return record

    async def _download_for_direct(self, urls: list) -> tuple:
        from preprocessor import _normalize_urls, download_image, _save_image, _classify_failure, MAX_RETRIES
        import asyncio, httpx

        dedup = list(dict.fromkeys(urls))
        img_infos = []
        url_images = {}

        async def fetch_one(url):
            urls_to_try = _normalize_urls(url)
            last_error = ""
            for norm_url in urls_to_try:
                for attempt in range(MAX_RETRIES):
                    try:
                        async with httpx.AsyncClient(verify=True, timeout=30) as c:
                            img_bytes = await download_image(c, norm_url, attempt)
                        local = _save_image(url, img_bytes)
                        return url, img_bytes, local, "done"
                    except Exception as e:
                        last_error = str(e).split("\n")[0][:120]
                        if "404" in last_error:
                            break
                        if any(x in last_error for x in ("502", "503", "504", "timed out")):
                            await asyncio.sleep(1.5 * (2 ** min(attempt, 3)))
                        elif attempt < MAX_RETRIES - 1:
                            await asyncio.sleep(1.0 * (2 ** min(attempt, 3)))
            return url, None, "", _classify_failure(last_error)

        results = await asyncio.gather(*[fetch_one(u) for u in dedup])
        for url, data, local, status in results:
            if data:
                url_images[url] = data
            img_infos.append({
                "url": url, "description": status if status != "done" else "已下载",
                "src": local, "status": "done" if data else "failed",
            })
        return url_images, img_infos

    def _build_direct_messages(self, title, options, question_type, url_images):
        from preprocessor import extract_image_urls

        type_label = {"single": "单选题", "multiple": "多选题", "judgement": "判断题", "completion": "填空题"}.get(question_type, question_type)
        raw_lines = [l for l in options.split("\n") if l.strip()]

        content = []
        cleaned_title = self._clean_title(title)
        text_parts = [f"题型：{type_label}"]
        title_urls = extract_image_urls(title)
        if title_urls:
            text_parts.append(f"题目：[见下图]")
        else:
            text_parts.append(f"题目：{cleaned_title}")
        content.append({"type": "text", "text": "\n".join(text_parts)})

        for u in title_urls:
            if u in url_images:
                img_b64 = base64.b64encode(url_images[u]).decode()
                content.append({"type": "image_url", "image_url": {"url": f"data:image/png;base64,{img_b64}"}})

        if question_type != "completion" and options.strip():
            opt_parts = ["选项："]
            for i, line in enumerate(raw_lines):
                stripped = line.strip()
                label = OPTION_LETTERS[i] if i < len(OPTION_LETTERS) else str(i)
                opt_urls = extract_image_urls(stripped)
                if opt_urls:
                    text_only = stripped
                    for u in opt_urls:
                        text_only = text_only.replace(u, "[图]")
                    opt_parts.append(f"{label}. {text_only}")
                else:
                    opt_parts.append(f"{label}. {stripped}")
            content.append({"type": "text", "text": "\n".join(opt_parts)})

            for i, line in enumerate(raw_lines):
                for u in extract_image_urls(line):
                    if u in url_images:
                        img_b64 = base64.b64encode(url_images[u]).decode()
                        label = OPTION_LETTERS[i] if i < len(OPTION_LETTERS) else str(i)
                        content.append({"type": "text", "text": f"[选项 {label} 的图片]"})
                        content.append({"type": "image_url", "image_url": {"url": f"data:image/png;base64,{img_b64}"}})

        content.append({"type": "text", "text": "请输出<answer>X</answer>"})
        return content

    def _build_direct_prompt_preview(self, title, options, question_type, url_images):
        type_label = {"single": "单选题", "multiple": "多选题", "judgement": "判断题", "completion": "填空题"}.get(question_type, question_type)
        lines = [f"题型：{type_label}", f"题目：{title}"]
        if options.strip():
            lines.append(f"选项：\n{options}")
        lines.append(f"\n[Direct 模式: 含 {len(url_images)} 张原图，直接发给视觉模型作答]")
        return "\n".join(lines)

    def _label_options(self, options: str) -> tuple:
        if not options.strip():
            return options, {}
        blocks = self._split_by_image_blocks(options)
        if blocks:
            return self._label_from_blocks(blocks)
        raw_lines = [l.strip() for l in options.split("\n") if l.strip()]
        OPTION_BOUNDARY = re.compile(r"^[A-Za-z][.\、)）:：\s]")
        any_has_label = any(OPTION_BOUNDARY.match(l) for l in raw_lines)
        if not any_has_label:
            label_map = {}
            labelled_lines = []
            for i, line in enumerate(raw_lines):
                label = OPTION_LETTERS[i] if i < len(OPTION_LETTERS) else str(i)
                label_map[label] = line
                labelled_lines.append(f"{label}. {line}")
            return "\n".join(labelled_lines), label_map
        groups = []
        current = []
        for line in raw_lines:
            if OPTION_BOUNDARY.match(line):
                if current:
                    groups.append(current)
                current = [line]
            else:
                current.append(line)
        if current:
            groups.append(current)
        label_map = {}
        labelled_lines = []
        for i, group in enumerate(groups):
            label = OPTION_LETTERS[i] if i < len(OPTION_LETTERS) else str(i)
            full = " ".join(group)
            c = re.sub(r"^[A-Za-z][.\、)）:：\s]*", "", full).strip()
            label_map[label] = c
            labelled_lines.append(f"{label}. {c}")
        return "\n".join(labelled_lines), label_map

    def _split_by_image_blocks(self, text: str) -> list:
        blocks = []
        i = 0
        while True:
            idx = text.find("[图片:", i)
            if idx == -1:
                break
            prefix = text[i:idx].strip()
            depth = 1
            j = idx + 1
            while j < len(text) and depth > 0:
                if text[j] == "\\" and j + 1 < len(text):
                    j += 2
                    continue
                if text[j] == "[":
                    depth += 1
                elif text[j] == "]":
                    depth -= 1
                j += 1
            block = text[idx:j]
            blocks.append((prefix, block))
            i = j
        return blocks

    def _label_from_blocks(self, blocks: list) -> tuple:
        label_map = {}
        labelled_lines = []
        LABEL_PAT = re.compile(r"^[A-Za-z][.\、)）:：\s]")
        for i, (prefix, block) in enumerate(blocks):
            label = OPTION_LETTERS[i] if i < len(OPTION_LETTERS) else str(i)
            if prefix and LABEL_PAT.match(prefix):
                orig_label = prefix.strip()
                inner = LABEL_PAT.sub("", orig_label).strip()
                label_map[label] = inner + " " + block if inner else block
            else:
                content = (prefix + " " + block).strip() if prefix else block
                label_map[label] = content
            labelled_lines.append(f"{label}. {label_map[label]}")
        return "\n".join(labelled_lines), label_map

    def _clean_title(self, text: str) -> str:
        text = re.sub(r"<[^>]+>", "", text)
        text = re.sub(r"&[a-z]+;", " ", text)
        text = re.sub(r"\s+", " ", text)
        return text.strip()

    def _build_prompt(self, title: str, options: str, question_type: str) -> str:
        type_label = {"single": "单选题", "multiple": "多选题", "judgement": "判断题", "completion": "填空题"}.get(question_type, question_type)
        title = self._clean_title(title)
        parts = [f"题型：{type_label}", f"题目：{title}"]
        if question_type != "completion" and options.strip():
            parts.append(f"选项：\n{options}")
        return "\n".join(parts)

    def _extract_tag(self, content: str, reasoning: str = "") -> str:
        m = ANSWER_TAG.search(content)
        raw = m.group(1).strip() if m else ""
        if not raw and reasoning:
            m = ANSWER_TAG.search(reasoning)
            raw = m.group(1).strip() if m else ""
        if not raw:
            return content.strip()
        if raw in ("对", "错"):
            return raw
        clean = re.sub(r"[^A-Za-z#]", "", raw)
        if not clean:
            return raw.strip()
        if "#" in clean:
            clean = re.sub(r"#+", "#", clean).strip("#")
            if re.match(r"^[A-Za-z](?:#[A-Za-z])*$", clean):
                return clean.upper()
        first = re.sub(r"[^A-Za-z]", "", raw.split("#")[0])
        if first:
            return first[0].upper()
        if "对" in raw:
            return "对"
        if "错" in raw:
            return "错"
        return raw.strip()

    async def test_text(self) -> bool:
        if self.text_model is None:
            raise RuntimeError("文本模型未配置：请在设置中选择 provider 并填写 base_url / api_key / model。")
        return await self.text_model.test()

    async def test_vision(self) -> bool:
        if self.vision_model is None:
            raise RuntimeError("视觉模型未配置：请在设置中选择 provider 并填写 base_url / api_key / model。")
        return await self.vision_model.test()

    def available_text_providers(self):
        return list_text_providers()

    def available_vision_providers(self):
        return list_vision_providers()


solver = Solver()
