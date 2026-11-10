"""HTTP-сервис поискового движка DocFinder.

Сервис использует стандартный http.server, поэтому образ остаётся небольшим.
Сервис внутренний, наружу из compose не публикуется. Если задан ENGINE_TOKEN, каждый запрос
должен нести заголовок Authorization: Bearer <ENGINE_TOKEN>.

    GET  /health                          -> {"status": "ok", "documents": N, "vectors": "..."}
    POST /answer                          -> ответ на вопрос с историей
    POST /reindex                         -> перечитать базу (каталог ENGINE_KB_PATH или переданные документы)
    GET  /documents                       -> список документов с метаданными
    GET  /documents/{doc_id}/structure    -> положения документа для просмотра в вебе

Тело /answer:
    {"question": "...", "history": [{"role": "user|assistant", "text": "..."}],
     "settings": {"base_url": "...", "api_key": "...", "llm_model": "...", "emb_model": "...",
                  "reranker_model": "...", "temperature": 0.0, "max_tokens": 1024}}
settings необязательны: по умолчанию используются переменные окружения.
"""
from __future__ import annotations

import hmac
import json
import os
import sys
import threading
import time
from dataclasses import asdict
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import unquote, urlparse

import dates
import kb as kb_mod
import kb_parse
from config import Config, load_config
from dialog import from_messages
from index import SearchIndex
from kb_loader import RawDocument
from llm_client import ApiError, LlmClient, log
from pipeline import Engine

MAX_BODY = 25 * 1024 * 1024
ANSWER_TIMEOUT = 110.0  # веб ждёт ядро до 120 с


def doc_summary(doc: kb_parse.Document) -> dict:
    return {
        "doc_id": doc.doc_id, "file_name": doc.file_name, "category": doc.category,
        "category_label": doc.category_label, "doc_type": doc.doc_type, "title": doc.title,
        "display_name": doc.display_name, "number": doc.number, "date": dates.fmt(doc.doc_date),
        "counterparty": doc.counterparty, "effective_from": dates.fmt(doc.effective_from),
        "effective_to": dates.fmt(doc.effective_to), "status_note": doc.status_note,
        "supplements": doc.supplements, "parent_id": doc.parent_id, "related": doc.related,
        "editions": doc.editions, "exported_at": dates.fmt(doc.exported_at),
    }


def raw_documents(items: list[dict]) -> list[RawDocument]:
    raws = []
    for item in items:
        text = str(item.get("text", "")).strip()
        if not text:
            continue
        name = str(item.get("file_name") or item.get("title") or "document.txt")
        category = str(item.get("category", "")).strip()
        stem = name.rsplit(".", 1)[0]
        if "__" in stem:
            prefix, stem = stem.split("__", 1)
            category = category or prefix
        raws.append(RawDocument(file_name=name, category=category, stem=stem, text=text))
    if not raws:
        raise ValueError("Нет документов с текстом")
    return raws


def settings_config(base: Config, settings: dict | None) -> Config:
    """Настройки модели из запроса (админка ядра) поверх окружения."""
    settings = settings or {}

    def pick(key: str, current):
        value = settings.get(key)
        return current if value in (None, "") else value

    api_key = str(pick("api_key", base.llm_key))
    reranker = str(pick("reranker_model", base.reranker_model))
    emb_model = str(pick("emb_model", base.emb_model))
    return base.with_overrides(
        base_url=str(pick("base_url", base.base_url)).rstrip("/"),
        llm_key=api_key, embed_key=api_key or base.embed_key, rerank_key=api_key or base.rerank_key,
        llm_model=str(pick("llm_model", base.llm_model)), emb_model=emb_model,
        reranker_model=reranker, use_reranker=bool(reranker),
        use_embeddings=bool(emb_model) and settings.get("embeddings", True) is not False,
        temperature=float(pick("temperature", base.temperature)),
        max_tokens=int(pick("max_tokens", base.max_tokens)))

class EngineService:
    def __init__(self, base_cfg: Config, kb_path: Path | None, cache_dir: Path):
        self.base_cfg = base_cfg
        self.kb_path = kb_path
        self.cache_dir = cache_dir
        self.lock = threading.RLock()
        self.kb: kb_mod.KnowledgeBase | None = None
        self.index: SearchIndex | None = None
        self.vector_model = ""
        self.clients: dict[tuple, LlmClient] = {}

    def load_from_path(self) -> None:
        if self.kb_path is None or not self.kb_path.exists():
            raise FileNotFoundError(f"База знаний не найдена: {self.kb_path}")
        self._install(kb_mod.load(self.kb_path))

    def load_from_documents(self, documents: list[dict]) -> None:
        self._install(kb_mod.build(kb_parse.parse_all(raw_documents(documents))))

    def _install(self, base: kb_mod.KnowledgeBase) -> None:
        index = SearchIndex(base)
        with self.lock:
            self.kb, self.index, self.vector_model = base, index, ""
        log(f"[server] база: {base.summary()}")

    def client_for(self, cfg: Config) -> LlmClient:
        key = (cfg.base_url, cfg.llm_key, cfg.llm_model, cfg.emb_model, cfg.reranker_model,
               cfg.temperature, cfg.max_tokens, cfg.use_embeddings)
        with self.lock:
            client = self.clients.get(key)
            if client is None:
                if len(self.clients) >= 8:
                    for old in self.clients.values():
                        old.close()
                    self.clients.clear()
                client = self.clients[key] = LlmClient(cfg)
            return client

    def ensure_vectors(self, client: LlmClient) -> None:
        """Векторы базы для текущей модели эмбеддингов; при ошибке остаётся BM25."""
        model = client.cfg.emb_model if client.available("embeddings") else ""
        with self.lock:
            if self.index is None or not model or model == self.vector_model:
                return
            safe = "".join(ch if ch.isalnum() else "_" for ch in model)
            if self.index.build_vectors(client, self.cache_dir / f"kb_vectors_{safe}.npz"):
                self.vector_model = model

    def answer(self, body: dict) -> dict:
        question = str(body.get("question", "")).strip()
        if not question:
            raise ValueError("Пустой вопрос")
        history = body.get("history") or []
        if not isinstance(history, list):
            raise ValueError("history должен быть списком")
        with self.lock:
            base, index = self.kb, self.index
        if base is None or index is None:
            raise RuntimeError("База знаний не загружена")
        cfg = settings_config(self.base_cfg, body.get("settings"))
        client = self.client_for(cfg)
        self.ensure_vectors(client)
        dialog = from_messages([item for item in history if isinstance(item, dict)], question)
        query_vector = None
        if index.vectors is not None:
            try:
                text = "\n".join([dialog.last_user] + [t.text for t in dialog.history if t.role == "user"])
                query_vector = client.embed([text])[0]
            except ApiError:
                query_vector = None
        engine = Engine(base, index, client if cfg.llm_ready else None, top_n=cfg.reranker_top_n)
        result = engine.answer(question, dialog=dialog, query_vector=query_vector,
                               deadline=time.monotonic() + ANSWER_TIMEOUT)
        payload = asdict(result)
        for citation in payload["citations"]:
            doc = base.by_id.get(citation["doc_id"])
            citation["file_name"] = doc.file_name if doc else ""
        return payload

    def documents(self) -> list[dict]:
        with self.lock:
            base = self.kb
        return [doc_summary(doc) for doc in base.docs] if base else []

    def structure(self, doc_id: str) -> dict | None:
        with self.lock:
            base = self.kb
        doc = base.by_id.get(doc_id) if base else None
        if doc is None:
            return None
        summary = doc_summary(doc)
        summary["chunks"] = [{"path": c.path, "section": c.section, "text": c.text, "notes": c.notes,
                              "kind": c.kind} for c in doc.chunks]
        summary["removed_lines"] = len(doc.removed_lines)
        return summary

def make_handler(service: EngineService, token: str):
    class Handler(BaseHTTPRequestHandler):
        server_version = "psb-engine"

        def log_message(self, fmt: str, *args) -> None:
            if os.getenv("DEBUG", "").lower() == "true":
                log("[server] " + fmt % args)

        def _send(self, status: int, payload) -> None:
            raw = json.dumps(payload, ensure_ascii=False).encode("utf-8")
            self.send_response(status)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Content-Length", str(len(raw)))
            self.send_header("X-Content-Type-Options", "nosniff")
            self.end_headers()
            self.wfile.write(raw)

        def _authorized(self) -> bool:
            if not token:
                return True
            header = self.headers.get("Authorization", "")
            return header.startswith("Bearer ") and hmac.compare_digest(header[7:].encode(), token.encode())

        def _body(self) -> dict:
            length = int(self.headers.get("Content-Length") or 0)
            if length <= 0:
                return {}
            if length > MAX_BODY:
                raise ValueError("Слишком большое тело запроса")
            body = json.loads(self.rfile.read(length).decode("utf-8"))
            if not isinstance(body, dict):
                raise ValueError("Ожидается JSON-объект")
            return body

        def _route(self, method: str) -> None:
            path = urlparse(self.path).path.rstrip("/") or "/"
            if path == "/health" and method == "GET":
                with service.lock:
                    count = len(service.kb.docs) if service.kb else 0
                self._send(200 if count else 503, {"status": "ok" if count else "no_kb", "documents": count,
                                                   "vectors": service.vector_model})
                return
            if not self._authorized():
                self._send(401, {"error": "unauthorized"})
                return
            try:
                if path == "/answer" and method == "POST":
                    self._send(200, service.answer(self._body()))
                elif path == "/reindex" and method == "POST":
                    body = self._body()
                    if body.get("documents"):
                        service.load_from_documents(body["documents"])
                    else:
                        service.load_from_path()
                    self._send(200, {"status": "ok", "documents": len(service.kb.docs)})
                elif path == "/documents" and method == "GET":
                    self._send(200, {"documents": service.documents()})
                elif path.startswith("/documents/") and path.endswith("/structure") and method == "GET":
                    doc_id = unquote(path[len("/documents/"):-len("/structure")])
                    structure = service.structure(doc_id)
                    self._send(200 if structure else 404, structure or {"error": "not_found"})
                else:
                    self._send(404, {"error": "not_found"})
            except (ValueError, json.JSONDecodeError) as exc:
                self._send(400, {"error": "bad_request", "detail": str(exc)})
            except FileNotFoundError as exc:
                self._send(404, {"error": "kb_not_found", "detail": str(exc)})
            except RuntimeError as exc:
                self._send(503, {"error": "unavailable", "detail": str(exc)})
            except Exception as exc:  # не отдаём трейс наружу
                log(f"[server] ошибка {type(exc).__name__}: {exc}")
                self._send(500, {"error": "internal"})

        def do_GET(self) -> None:
            self._route("GET")

        def do_POST(self) -> None:
            self._route("POST")

    return Handler


def create_server(service: EngineService, host: str, port: int, token: str = "") -> ThreadingHTTPServer:
    server = ThreadingHTTPServer((host, port), make_handler(service, token))
    server.daemon_threads = True
    return server


def main() -> None:
    for stream in (sys.stdout, sys.stderr):
        try:
            stream.reconfigure(encoding="utf-8")
        except (AttributeError, ValueError):
            pass
    kb_path = os.getenv("ENGINE_KB_PATH", "").strip()
    cache_dir = Path(os.getenv("ENGINE_CACHE_DIR", "").strip() or (Path.cwd() / "cache"))
    service = EngineService(load_config(), Path(kb_path) if kb_path else None, cache_dir)
    if service.kb_path:
        try:
            service.load_from_path()
        except (FileNotFoundError, ValueError) as exc:
            log(f"[server] база не загружена: {exc}; ждём POST /reindex")
    host = os.getenv("ENGINE_HOST", "0.0.0.0")
    port = int(os.getenv("ENGINE_PORT", "8000"))
    token = os.getenv("ENGINE_TOKEN", "").strip()
    if not token:
        log("[server] ENGINE_TOKEN не задан: сервис принимает запросы без авторизации, "
            "держите его только во внутренней сети")
    server = create_server(service, host, port, token)
    log(f"[server] слушаю {host}:{port}")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
