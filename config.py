import os
import sys
from pathlib import Path
from typing import Any, Dict

import yaml


def exe_dir() -> Path:
    if getattr(sys, "frozen", False):
        return Path(sys.executable).parent
    return Path(__file__).resolve().parent


def bundle_dir() -> Path:
    if getattr(sys, "frozen", False):
        return Path(getattr(sys, "_MEIPASS", ""))
    return Path(__file__).resolve().parent


class Config:
    def __init__(self, config_path: Path = None):
        if config_path is None:
            config_path = exe_dir() / "config.yaml"
        self._path = config_path
        self._data: Dict[str, Any] = {}
        self.load()

    def _defaults(self) -> dict:
        return {
            "server": {"host": "127.0.0.1", "port": 8000},
            "models": {
                "text": {
                    "provider": "deepseek",
                    "model": "deepseek-v4-flash",
                    "api_key": "",
                    "base_url": "https://api.deepseek.com",
                    "reasoning_effort": "max",
                },
                "vision": {
                    "provider": "",
                    "model": "gpt-5.4-mini",
                    "api_key": "",
                    "base_url": "https://api.openai.com/v1",
                },
            },
            "answer": {
                "mode": "stepwise",
                "temperature": 0.1,
                "system_prompt": (
                    "你是一个大学生网课答题助手。请根据题目和选项，在<answer>标签内输出正确答案。\n\n"
                    "格式要求：\n"
                    "单选题：<answer>A</answer>\n"
                    "多选题：<answer>A#B#C</answer>\n"
                    "判断题：<answer>对</answer> 或 <answer>错</answer>\n"
                    "填空题：<answer>填空内容</answer>（多个空用#分隔）\n\n"
                    "只输出<answer>标签，禁止任何额外文字、分析或解释。"
                ),
            },
        }

    def load(self):
        if self._path.exists():
            with open(self._path, "r", encoding="utf-8") as f:
                loaded = yaml.safe_load(f)
                if loaded and isinstance(loaded, dict):
                    self._data = self._merge_defaults(loaded)
                else:
                    self._data = self._defaults()
        else:
            self._data = self._defaults()
            self.save()
        self._apply_env_overrides()

    def _merge_defaults(self, loaded: dict) -> dict:
        defaults = self._defaults()
        for section in ["server", "models", "answer"]:
            if section not in loaded:
                loaded[section] = defaults.get(section, {})
            elif isinstance(loaded[section], dict) and isinstance(defaults.get(section), dict):
                if section == "models":
                    for mtype in ["text", "vision"]:
                        if mtype not in loaded["models"]:
                            loaded["models"][mtype] = defaults["models"].get(mtype, {})
                        elif isinstance(loaded["models"][mtype], dict):
                            ds = defaults["models"].get(mtype, {})
                            for k, v in ds.items():
                                loaded["models"][mtype].setdefault(k, v)
                else:
                    for k, v in defaults[section].items():
                        loaded[section].setdefault(k, v)
        return loaded

    def save(self):
        self._path.parent.mkdir(parents=True, exist_ok=True)
        with open(self._path, "w", encoding="utf-8") as f:
            yaml.dump(self._data, f, allow_unicode=True, default_flow_style=False)

    def _apply_env_overrides(self):
        env_map = {
            "OCS_TEXT_API_KEY": ("models", "text", "api_key"),
            "OCS_TEXT_BASE_URL": ("models", "text", "base_url"),
            "OCS_TEXT_MODEL": ("models", "text", "model"),
            "OCS_VISION_API_KEY": ("models", "vision", "api_key"),
            "OCS_VISION_BASE_URL": ("models", "vision", "base_url"),
            "OCS_VISION_MODEL": ("models", "vision", "model"),
            "OCS_PORT": ("server", "port"),
        }
        for env_key, path_tuple in env_map.items():
            val = os.environ.get(env_key)
            if val:
                self._set_nested(path_tuple, val)

    def _set_nested(self, keys: tuple, value: Any):
        d = self._data
        for k in keys[:-1]:
            d = d.setdefault(k, {})
        if keys[-1] == "port":
            value = int(value)
        d[keys[-1]] = value

    def _get_nested(self, keys: tuple, default=None) -> Any:
        d = self._data
        for k in keys:
            if not isinstance(d, dict) or k not in d:
                return default
            d = d[k]
        return d

    def get_text_config(self) -> dict:
        return dict(self._data.get("models", {}).get("text", {}))

    def get_vision_config(self) -> dict:
        return dict(self._data.get("models", {}).get("vision", {}))

    def set_text_config(self, **kwargs):
        self._data.setdefault("models", {}).setdefault("text", {}).update(kwargs)

    def set_vision_config(self, **kwargs):
        self._data.setdefault("models", {}).setdefault("vision", {}).update(kwargs)

    @property
    def server_host(self) -> str:
        return str(self._data.get("server", {}).get("host", "127.0.0.1"))

    @property
    def server_port(self) -> int:
        return int(self._data.get("server", {}).get("port", 8000))

    @property
    def answer_config(self) -> dict:
        return dict(self._data.get("answer", {}))

    @property
    def raw_data(self) -> dict:
        return dict(self._data)


config = Config()
