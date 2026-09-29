from abc import ABC, abstractmethod
from typing import Optional, Tuple


class TextModel(ABC):
    def __init__(self, model: str, api_key: str, base_url: str, **kwargs):
        self.model = model
        self.api_key = api_key
        self.base_url = base_url
        self.extra = kwargs

    @abstractmethod
    async def chat(self, system_prompt: str, user_message: str) -> Tuple[str, str]: ...

    @abstractmethod
    async def test(self) -> bool: ...

    @property
    @abstractmethod
    def display_name(self) -> str: ...


class VisionModel(ABC):
    def __init__(self, model: str, api_key: str, base_url: str, **kwargs):
        self.model = model
        self.api_key = api_key
        self.base_url = base_url
        self.extra = kwargs

    @abstractmethod
    async def describe(self, image_bytes: bytes, prompt: str) -> str: ...

    @abstractmethod
    async def test(self) -> bool: ...

    @property
    @abstractmethod
    def display_name(self) -> str: ...
