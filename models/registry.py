from typing import Dict, Type

from .base import TextModel, VisionModel

TEXT_REGISTRY: Dict[str, Type[TextModel]] = {}
VISION_REGISTRY: Dict[str, Type[VisionModel]] = {}


def register_text(provider: str, cls: Type[TextModel]):
    TEXT_REGISTRY[provider] = cls


def register_vision(provider: str, cls: Type[VisionModel]):
    VISION_REGISTRY[provider] = cls


def list_text_providers():
    return list(TEXT_REGISTRY.keys())


def list_vision_providers():
    return list(VISION_REGISTRY.keys())


def get_text_provider(name: str) -> Type[TextModel]:
    if name not in TEXT_REGISTRY:
        raise ValueError(f"Unknown text provider: {name}. Available: {list_text_providers()}")
    return TEXT_REGISTRY[name]


def get_vision_provider(name: str) -> Type[VisionModel]:
    if name not in VISION_REGISTRY:
        raise ValueError(f"Unknown vision provider: {name}. Available: {list_vision_providers()}")
    return VISION_REGISTRY[name]
