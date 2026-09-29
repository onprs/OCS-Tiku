from typing import Optional, Tuple

from openai import AsyncOpenAI

from ..base import TextModel
from ..registry import register_text


class DeepSeekModel(TextModel):
    def __init__(self, model: str = "deepseek-v4-flash", api_key: str = "", base_url: str = "https://api.deepseek.com", reasoning_effort: str = "max", **kwargs):
        super().__init__(model, api_key, base_url, **kwargs)
        self.reasoning_effort = reasoning_effort
        self._client: Optional[AsyncOpenAI] = None

    @property
    def client(self) -> AsyncOpenAI:
        if self._client is None:
            self._client = AsyncOpenAI(api_key=self.api_key, base_url=self.base_url)
        return self._client

    @property
    def display_name(self) -> str:
        return f"DeepSeek ({self.model})"

    async def chat(self, system_prompt: str, user_message: str) -> Tuple[str, str]:
        resp = await self.client.chat.completions.create(
            model=self.model,
            messages=[
                {"role": "system", "content": system_prompt},
                {"role": "user", "content": user_message},
            ],
            reasoning_effort=self.reasoning_effort,
            extra_body={"thinking": {"type": "enabled"}},
        )
        msg = resp.choices[0].message
        return (msg.content or ""), (getattr(msg, "reasoning_content", None) or "")

    async def test(self) -> bool:
        await self.chat("回复OK", "OK")
        return True


register_text("deepseek", DeepSeekModel)
