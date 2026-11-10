"""Клиент OpenAI-совместимого API: генерация, эмбеддинги, реранкер.

Один протокол для всех провайдеров:
- платформа ПСБ: локальный прокси из LLM_BASE_URL;
- своё API (OpenRouter, vLLM и т. п.): base URL и ключ из настроек;
- Ollama: base URL http://host:11434/v1, без ключа и без /rerank.

Не используем tools, response_format и прочие возможности, которых может не быть у модели.
Ключи не логируются. Логи только в stderr.
"""
from __future__ import annotations

import math
import random
import sys
import threading
import time
from typing import Sequence

import httpx

from config import Config

RETRY_STATUSES = {408, 409, 425, 429, 500, 502, 503, 504}
MIN_CHAT_TOKENS = 3000
MAX_CHAT_TOKENS = 8192


class ApiError(Exception):
    """Сервис не ответил после всех попыток или ответ не разобран."""


def log(message: str) -> None:
    print(message, file=sys.stderr, flush=True)


class LlmClient:
    def __init__(self, cfg: Config, transport: httpx.BaseTransport | None = None):
        self.cfg = cfg
        self._client = httpx.Client(timeout=cfg.request_timeout, transport=transport)
        self._lock = threading.Lock()
        # Сервис, который упал на конфигурации (404 на эндпоинт), больше не вызываем
        self._disabled: set[str] = set()

    def close(self) -> None:
        self._client.close()

    def __enter__(self) -> "LlmClient":
        return self

    def __exit__(self, *exc) -> None:
        self.close()

    def available(self, service: str) -> bool:
        if not self.cfg.base_url:
            return False
        if service == "chat" and not self.cfg.llm_model:
            return False
        if service == "embeddings" and (not self.cfg.use_embeddings or not self.cfg.emb_model):
            return False
        if service == "rerank" and (not self.cfg.use_reranker or not self.cfg.reranker_model):
            return False
        with self._lock:
            return service not in self._disabled

    def disable(self, service: str, reason: str) -> None:
        with self._lock:
            if service not in self._disabled:
                self._disabled.add(service)
                log(f"[llm] {service} отключён до конца прогона: {reason}")

    def _post(self, service: str, path: str, key: str, payload: dict, attempts: int = 3,
              deadline: float | None = None) -> dict:
        headers = {"Content-Type": "application/json"}
        if key:
            headers["Authorization"] = "Bearer " + key
        url = self.cfg.base_url + path
        last_error = "нет попыток"
        for attempt in range(attempts):
            if deadline is not None and time.monotonic() > deadline:
                raise ApiError("истёк бюджет времени")
            try:
                response = self._client.post(url, headers=headers, json=payload)
            except httpx.HTTPError as exc:
                last_error = type(exc).__name__
            else:
                if response.status_code < 300:
                    try:
                        return response.json()
                    except ValueError:
                        last_error = "ответ не JSON"
                elif response.status_code in RETRY_STATUSES:
                    last_error = f"HTTP {response.status_code}"
                else:
                    # 400/401/404 повтором не лечатся; недоступный эмбеддер/реранкер больше не зовём
                    if response.status_code in (401, 403, 404, 405, 501) and service != "chat":
                        self.disable(service, f"HTTP {response.status_code}")
                    raise ApiError(f"{service}: HTTP {response.status_code}")
            if attempt + 1 < attempts:
                time.sleep(min(8.0, 2 ** attempt) + random.random() * 0.3)
        raise ApiError(f"{service}: {last_error}")

    # --- генерация ---

    def chat(self, messages: list[dict], max_tokens: int | None = None,
             temperature: float | None = None, deadline: float | None = None) -> str:
        if not self.available("chat"):
            raise ApiError("chat недоступен")
        payload = {
            "model": self.cfg.llm_model,
            "messages": messages,
            "temperature": self.cfg.temperature if temperature is None else temperature,
            # Рассуждающие модели тратят часть лимита на скрытые рассуждения: нижняя граница выше
            "max_tokens": max(max_tokens or self.cfg.max_tokens, MIN_CHAT_TOKENS),
        }
        # Пустой content бывает у рассуждающих моделей при нехватке max_tokens: повтор с большим лимитом
        for _ in range(2):
            result = self._post("chat", "/chat/completions", self.cfg.llm_key, payload, deadline=deadline)
            content = _chat_content(result)
            if content:
                return content
            payload = {**payload, "max_tokens": min(payload["max_tokens"] * 2, MAX_CHAT_TOKENS)}
        raise ApiError("chat: пустой ответ")

    # --- эмбеддинги ---

    def embed(self, texts: Sequence[str], batch_size: int = 32,
              deadline: float | None = None) -> list[list[float]]:
        if not self.available("embeddings"):
            raise ApiError("embeddings недоступны")
        vectors: list[list[float]] = []
        for start in range(0, len(texts), batch_size):
            batch = [text[:8000] for text in texts[start:start + batch_size]]
            result = self._post("embeddings", "/embeddings", self.cfg.embed_key, {
                "model": self.cfg.emb_model,
                "input": batch,
                "encoding_format": "float",
            }, deadline=deadline)
            vectors.extend(parse_embeddings(result, len(batch)))
        return vectors

    # --- реранкер ---

    def rerank(self, query: str, documents: Sequence[str], top_n: int,
               deadline: float | None = None) -> list[tuple[int, float]]:
        """Индексы документов по убыванию релевантности и их оценки."""
        if not self.available("rerank"):
            raise ApiError("rerank недоступен")
        if not documents:
            return []
        result = self._post("rerank", "/rerank", self.cfg.rerank_key, {
            "model": self.cfg.reranker_model,
            "query": query,
            "documents": [doc[:4000] for doc in documents],
            "top_n": min(top_n, len(documents)),
        }, attempts=2, deadline=deadline)
        ranked = parse_rerank(result, len(documents))
        if not ranked:
            raise ApiError("rerank: пустой результат")
        return ranked


def _chat_content(result: dict) -> str:
    try:
        content = result["choices"][0]["message"].get("content") or ""
    except (KeyError, IndexError, TypeError, AttributeError):
        return ""
    if isinstance(content, list):
        content = "".join(part.get("text", "") for part in content if isinstance(part, dict))
    return content.strip() if isinstance(content, str) else ""


def parse_embeddings(result: dict, expected: int) -> list[list[float]]:
    try:
        data = result["data"]
    except (KeyError, TypeError):
        raise ApiError("embeddings: нет поля data")
    vectors: list[list[float] | None] = [None] * expected
    for position, item in enumerate(data):
        try:
            index = int(item.get("index", position))
            raw = item["embedding"]
        except (KeyError, TypeError, ValueError, AttributeError):
            raise ApiError("embeddings: неверный элемент")
        if not 0 <= index < expected:
            raise ApiError("embeddings: индекс вне диапазона")
        norm = math.sqrt(sum(float(x) * float(x) for x in raw)) or 1.0
        vectors[index] = [float(x) / norm for x in raw]
    if any(vector is None for vector in vectors):
        raise ApiError("embeddings: получены не все векторы")
    return vectors  # type: ignore[return-value]


def parse_rerank(result: dict, count: int) -> list[tuple[int, float]]:
    items = result.get("results", result.get("data", [])) if isinstance(result, dict) else []
    ranked: list[tuple[int, float]] = []
    seen: set[int] = set()
    for item in items if isinstance(items, list) else []:
        try:
            index = int(item["index"])
        except (KeyError, TypeError, ValueError):
            continue
        if not 0 <= index < count or index in seen:
            continue
        seen.add(index)
        try:
            score = float(item.get("relevance_score", item.get("score", 0.0)))
        except (TypeError, ValueError):
            score = 0.0
        ranked.append((index, score))
    return ranked
