from typing import Optional, Tuple

from openai import AsyncOpenAI

from ..base import TextModel
from ..registry import register_text


class OpenAICompatModel(TextModel):
    def __init__(self, model: str = "qwen-plus", api_key: str = "", base_url: str = "https://dashscope.aliyuncs.com/compatible-mode/v1", **kwargs):
        super().__init__(model, api_key, base_url, **kwargs)
        self._client: Optional[AsyncOpenAI] = None

    @property
    def client(self) -> AsyncOpenAI:
        if self._client is None:
            self._client = AsyncOpenAI(api_key=self.api_key, base_url=self.base_url)
        return self._client

    @property
    def display_name(self) -> str:
        return f"OpenAI Compat ({self.model})"

    async def chat(self, system_prompt: str, user_message: str) -> Tuple[str, str]:
        resp = await self.client.chat.completions.create(
            model=self.model,
            messages=[
                {"role": "system", "content": system_prompt},
                {"role": "user", "content": user_message},
            ],
            temperature=0.1,
        )
        msg = resp.choices[0].message
        return (msg.content or ""), ""

    async def test(self) -> bool:
        await self.chat("回复OK", "OK")
        return True


register_text("openai_compat", OpenAICompatModel)
