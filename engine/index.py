"""Поисковый индекс: BM25 по основам слов + косинус по векторам bge-m3, слияние RRF.

BM25 свой (без внешних пакетов). Векторы считаются через /embeddings при запуске
и кэшируются в файл по sha256 текста и имени модели: повторный запуск не пересчитывает базу.
Если эмбеддер недоступен, индекс работает только на BM25.
"""
from __future__ import annotations

import hashlib
import math
import sys
import time
from collections import Counter
from pathlib import Path

import numpy as np

import textnorm
from kb import KnowledgeBase
from kb_parse import Chunk
from llm_client import ApiError, LlmClient

RRF_K = 60


def chunk_search_text(kb: KnowledgeBase, chunk: Chunk) -> str:
    """Текст фрагмента для поиска: название документа, раздел, путь и сам текст."""
    doc = kb.doc(chunk)
    head = [doc.display_name, doc.title if doc.title != doc.display_name else "", chunk.section, chunk.path]
    if doc.counterparty:
        head.append(doc.counterparty)
    return "\n".join(part for part in head if part) + "\n" + chunk.text


class Bm25:
    def __init__(self, documents: list[list[str]], k1: float = 1.4, b: float = 0.7):
        self.k1, self.b = k1, b
        self.freqs = [Counter(doc) for doc in documents]
        self.lengths = [len(doc) for doc in documents]
        self.avg = (sum(self.lengths) / len(self.lengths)) if documents else 1.0
        df: Counter[str] = Counter()
        for freq in self.freqs:
            df.update(freq.keys())
        total = len(documents)
        self.idf = {term: math.log(1 + (total - count + 0.5) / (count + 0.5)) for term, count in df.items()}

    def scores(self, query: list[str]) -> np.ndarray:
        result = np.zeros(len(self.freqs), dtype=np.float32)
        terms = [term for term in set(query) if term in self.idf]
        for index, freq in enumerate(self.freqs):
            norm = self.k1 * (1 - self.b + self.b * self.lengths[index] / self.avg)
            score = 0.0
            for term in terms:
                tf = freq.get(term)
                if tf:
                    score += self.idf[term] * tf * (self.k1 + 1) / (tf + norm)
            result[index] = score
        return result


class SearchIndex:
    def __init__(self, kb: KnowledgeBase):
        self.kb = kb
        self.texts = [chunk_search_text(kb, chunk) for chunk in kb.chunks]
        self.bm25 = Bm25([textnorm.tokens(text) for text in self.texts])
        self.vectors: np.ndarray | None = None
        self.model = ""

    def build_vectors(self, client: LlmClient, cache_path: Path | None = None,
                      deadline: float | None = None) -> bool:
        if not client.available("embeddings"):
            return False
        model = client.cfg.emb_model
        keys = [hashlib.sha256((model + "\n" + text).encode("utf-8")).hexdigest() for text in self.texts]
        cached = load_cache(cache_path)
        missing = [i for i, key in enumerate(keys) if key not in cached]
        started = time.monotonic()
        if missing:
            try:
                fresh = client.embed([self.texts[i] for i in missing], deadline=deadline)
            except ApiError as exc:
                print(f"[index] эмбеддинги недоступны: {exc}; поиск только по BM25", file=sys.stderr)
                return False
            for i, vector in zip(missing, fresh):
                cached[keys[i]] = np.asarray(vector, dtype=np.float32)
            save_cache(cache_path, {key: cached[key] for key in keys})
        if len({cached[key].shape[0] for key in keys}) != 1:
            print("[index] векторы разной длины, поиск только по BM25", file=sys.stderr)
            return False
        self.vectors = np.stack([cached[key] for key in keys])
        self.model = model
        print(f"[index] векторы: {len(keys)} (новых {len(missing)}) за {time.monotonic() - started:.1f} с",
              file=sys.stderr)
        return True

    def bm25_scores(self, query: str) -> np.ndarray:
        return self.bm25.scores(textnorm.tokens(query))

    def vector_scores(self, query_vector: list[float] | None) -> np.ndarray | None:
        if self.vectors is None or query_vector is None:
            return None
        vector = np.asarray(query_vector, dtype=np.float32)
        if vector.shape[0] != self.vectors.shape[1]:
            return None
        return self.vectors @ vector

    def search(self, query: str, query_vector: list[float] | None = None, allowed: set[int] | None = None,
               limit: int = 30) -> list[tuple[int, float]]:
        """Индексы фрагментов и RRF-оценка, лучшие сначала. allowed - ограничение по фрагментам."""
        candidates = sorted(allowed) if allowed is not None else list(range(len(self.texts)))
        if not candidates:
            return []
        fused: dict[int, float] = {}
        bm25 = self.bm25_scores(query)
        for rank, index in enumerate(sorted(candidates, key=lambda i: -bm25[i])[:limit * 2]):
            if bm25[index] > 0:
                fused[index] = fused.get(index, 0.0) + 1.0 / (RRF_K + rank + 1)
        dense = self.vector_scores(query_vector)
        if dense is not None:
            for rank, index in enumerate(sorted(candidates, key=lambda i: -dense[i])[:limit * 2]):
                fused[index] = fused.get(index, 0.0) + 1.0 / (RRF_K + rank + 1)
        return sorted(fused.items(), key=lambda item: -item[1])[:limit]


def load_cache(path: Path | None) -> dict[str, np.ndarray]:
    if path is None or not path.is_file():
        return {}
    try:
        with np.load(path, allow_pickle=False) as data:
            keys = [str(key) for key in data["keys"]]
            vectors = data["vectors"]
        return {key: vectors[i] for i, key in enumerate(keys)}
    except (OSError, KeyError, ValueError) as exc:
        print(f"[index] кэш векторов не прочитан: {type(exc).__name__}", file=sys.stderr)
        return {}


def save_cache(path: Path | None, items: dict[str, np.ndarray]) -> None:
    if path is None or not items:
        return
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        tmp = path.with_name(path.stem + ".tmp.npz")
        np.savez(tmp, keys=np.array(list(items.keys())), vectors=np.stack(list(items.values())))
        tmp.replace(path)
    except (OSError, ValueError) as exc:
        print(f"[index] кэш векторов не сохранён: {type(exc).__name__}", file=sys.stderr)
