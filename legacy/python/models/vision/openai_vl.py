import base64
from io import BytesIO
from typing import Optional, Tuple

from openai import AsyncOpenAI

from ..base import VisionModel
from ..registry import register_vision


class OpenAIVLModel(VisionModel):
    def __init__(self, model: str = "gpt-5.4-mini", api_key: str = "", base_url: str = "https://api.openai.com/v1", **kwargs):
        super().__init__(model, api_key, base_url, **kwargs)
        self._client: Optional[AsyncOpenAI] = None

    @property
    def client(self) -> AsyncOpenAI:
        if self._client is None:
            self._client = AsyncOpenAI(api_key=self.api_key, base_url=self.base_url)
        return self._client

    @property
    def display_name(self) -> str:
        return f"OpenAI VL ({self.model})"

    async def describe(self, image_bytes: bytes, prompt: str) -> str:
        buffered = BytesIO(image_bytes)
        img_b64 = base64.b64encode(buffered.read()).decode("utf-8")

        resp = await self.client.chat.completions.create(
            model=self.model,
            messages=[
                {
                    "role": "user",
                    "content": [
                        {"type": "image_url", "image_url": {"url": f"data:image/png;base64,{img_b64}"}},
                        {"type": "text", "text": prompt},
                    ],
                }
            ],
            temperature=0.1,
            max_tokens=512,
        )
        return resp.choices[0].message.content or ""

    async def direct_answer(self, system_prompt: str, user_content: list) -> Tuple[str, str]:
        resp = await self.client.chat.completions.create(
            model=self.model,
            messages=[
                {"role": "system", "content": system_prompt},
                {"role": "user", "content": user_content},
            ],
            temperature=0.1,
        )
        msg = resp.choices[0].message
        return (msg.content or ""), ""

    async def test(self) -> bool:
        result = await self.describe(self._dummy_image(), "What text do you see? Reply 'OK' only.")
        return len(result) > 0

    def _dummy_image(self) -> bytes:
        from PIL import Image
        img = Image.new("RGB", (100, 30), color="white")
        buf = BytesIO()
        img.save(buf, format="PNG")
        return buf.getvalue()


register_vision("openai_vl", OpenAIVLModel)
