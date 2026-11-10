"""Фейковый OpenAI-совместимый сервер для тестов без настоящей модели.

/embeddings - детерминированные векторы по хэшам основ слов (похожие тексты -> похожие векторы);
/rerank     - порядок по пересечению слов;
/chat/completions - ответ в формате движка по первому фрагменту (или «плохой» ответ в режиме hallucinate).
Счётчики запросов доступны в FakeOpenAI.calls.
"""
from __future__ import annotations

import hashlib
import json
import math
import re
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import textnorm

DIMS = 64


def fake_vector(text: str) -> list[float]:
    vector = [0.0] * DIMS
    for token in textnorm.tokens(text):
        digest = hashlib.md5(token.encode("utf-8")).digest()
        vector[digest[0] % DIMS] += 1.0 if digest[1] % 2 else -1.0
    norm = math.sqrt(sum(x * x for x in vector)) or 1.0
    return [x / norm for x in vector]


class FakeOpenAI:
    def __init__(self, mode: str = "good", fail: set[str] | None = None):
        self.mode = mode            # good | hallucinate | unformatted
        self.fail = fail or set()   # эндпоинты, которые отвечают 500/404
        self.calls: dict[str, int] = {}
        self.last_messages: list[dict] = []
        handler = self._handler()
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    @property
    def url(self) -> str:
        return f"http://127.0.0.1:{self.server.server_address[1]}/v1"

    def __enter__(self) -> "FakeOpenAI":
        self.thread.start()
        return self

    def __exit__(self, *exc) -> None:
        self.server.shutdown()
        self.server.server_close()

    def chat_reply(self, messages: list[dict]) -> str:
        self.last_messages = messages
        content = messages[-1]["content"] if len(messages) <= 2 else messages[1]["content"]
        if self.mode == "unformatted":
            return "Просто текст без формата."
        fragments = re.findall(r'<fragment id="(F\d+)">.*?Текст:\n(.*?)\n</fragment>', content, re.S)
        question = re.search(r"<question>\n(.*?)\n</question>", content, re.S)
        if not fragments or not question:
            return "KIND: refusal\nANSWER: В базе нет документов по вопросу.\nMISSING: документы\nUSED: -"
        # Самый близкий по словам фрагмент (не шапка): так фейк ведёт себя похоже на модель
        words = textnorm.content_stems(question.group(1))
        label, text = max(fragments, key=lambda item: len(words & textnorm.content_stems(item[1]))
                          - (5 if item[1].isupper() or "именуемое" in item[1] else 0))
        flat = re.sub(r"^\d+(?:\.\d+)*\.\s*", "", text.replace("\n", " "))
        sentence = re.split(r"(?<=[.;])\s", flat)[0][:300]
        repairing = len(messages) > 2
        if self.mode == "hallucinate" and not repairing:
            return f"KIND: answer\nANSWER: Срок составляет 777 дней по п. 99.9 [{label}].\nMISSING: -\nUSED: {label}"
        return f"KIND: answer\nANSWER: {sentence} [{label}]\nMISSING: -\nUSED: {label}"

    def _handler(self):
        fake = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args) -> None:
                pass

            def do_POST(self) -> None:
                length = int(self.headers.get("Content-Length", "0"))
                body = json.loads(self.rfile.read(length) or b"{}")
                path = self.path.replace("/v1", "", 1)
                fake.calls[path] = fake.calls.get(path, 0) + 1
                if path in fake.fail:
                    self._send(404 if path == "/rerank" else 500, {"error": "fail"})
                    return
                if path == "/embeddings":
                    data = [{"index": i, "embedding": fake_vector(text)} for i, text in enumerate(body["input"])]
                    self._send(200, {"data": data})
                elif path == "/rerank":
                    query = textnorm.content_stems(body["query"])
                    scored = sorted(((len(query & textnorm.content_stems(doc)), i)
                                     for i, doc in enumerate(body["documents"])), reverse=True)
                    results = [{"index": i, "relevance_score": float(s)} for s, i in scored[:body.get("top_n", 5)]]
                    self._send(200, {"results": results})
                elif path == "/chat/completions":
                    # Второй и последующие сообщения - повтор после проверки: тоже отвечаем
                    content = fake.chat_reply(body["messages"])
                    self._send(200, {"choices": [{"message": {"role": "assistant", "content": content}}]})
                else:
                    self._send(404, {"error": "not found"})

            def _send(self, status: int, payload: dict) -> None:
                raw = json.dumps(payload, ensure_ascii=False).encode("utf-8")
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(raw)))
                self.end_headers()
                self.wfile.write(raw)

        return Handler
