"""Настройки поискового движка DocFinder из переменных окружения.

Значения ключей никуда не пишутся и не логируются.
"""
from __future__ import annotations

import os
from dataclasses import dataclass, field, replace


def env(name: str, default: str = "") -> str:
    # Оркестратор может оставить в переменной её же имя вместо значения: считаем это пустым
    value = os.getenv(name, "").strip()
    return default if not value or value == name else value


def env_first(*names: str, default: str = "") -> str:
    for name in names:
        value = env(name)
        if value:
            return value
    return default


def env_int(name: str, default: int, low: int, high: int) -> int:
    try:
        return max(low, min(high, int(env(name))))
    except ValueError:
        return default


def env_float(name: str, default: float, low: float = 0.0, high: float = 2.0) -> float:
    try:
        return max(low, min(high, float(env(name))))
    except ValueError:
        return default


@dataclass(frozen=True)
class Config:
    base_url: str = ""
    llm_key: str = field(default="", repr=False)
    embed_key: str = field(default="", repr=False)
    rerank_key: str = field(default="", repr=False)
    llm_model: str = ""
    emb_model: str = "baai/bge-m3"
    reranker_model: str = ""
    # Сколько фрагментов уходит в модель после поиска и реранкера
    top_k: int = 8
    reranker_top_n: int = 8
    temperature: float = 0.0
    max_tokens: int = 1024
    request_timeout: float = 60.0
    # auto: отдельная самопроверка только при подозрительном ответе
    self_check: str = "auto"
    use_embeddings: bool = True
    use_reranker: bool = True

    @property
    def llm_ready(self) -> bool:
        return bool(self.base_url and self.llm_model)

    def with_overrides(self, **values) -> "Config":
        return replace(self, **{k: v for k, v in values.items() if v is not None})


def load_config() -> Config:
    base_url = env_first("LLM_BASE_URL", "OPENAI_BASE_URL", "BASE_URL").rstrip("/")
    llm_key = env_first("LLM_API_KEY", "OPENAI_API_KEY")
    reranker_model = env("RERANKER_MODEL")
    return Config(
        base_url=base_url,
        llm_key=llm_key,
        embed_key=env("EMBEDDER_API_KEY") or llm_key,
        rerank_key=env("RERANKER_API_KEY") or llm_key,
        llm_model=env_first("LLM_MODEL", "OPENAI_MODEL", "GEN_MODEL"),
        emb_model=env("EMB_MODEL", "baai/bge-m3"),
        reranker_model=reranker_model,
        # Индекс хранит отдельные положения, поэтому значения ниже 6 поднимаем:
        # иначе теряются соседние пункты документа.
        top_k=max(6, env_int("TOP_K", 8, 1, 20)),
        reranker_top_n=max(6, env_int("RERANKER_TOP_N", 8, 1, 20)),
        temperature=env_float("TEMPERATURE", 0.0),
        max_tokens=env_int("MAX_TOKENS", 1024, 256, 8192),
        request_timeout=env_float("ENGINE_REQUEST_TIMEOUT", 60.0, 5, 180),
        self_check=env("SELF_CHECK", "auto").lower(),
        use_embeddings=env("ENGINE_EMBEDDINGS", "on").lower() != "off",
        use_reranker=bool(reranker_model) and env("ENGINE_RERANKER", "on").lower() != "off",
    )
