"""Локальный эмбеддер не получает ключ генеративного API."""
import unittest

import httpx

from config import Config
from llm_client import LlmClient


class SplitProviders(unittest.TestCase):
    def test_chat_and_embeddings_use_separate_urls(self):
        seen = []

        def respond(request):
            seen.append((str(request.url), request.headers.get("authorization")))
            if request.url.path.endswith("/embeddings"):
                return httpx.Response(200, json={"data": [{"index": 0, "embedding": [3, 4]}]})
            return httpx.Response(200, json={"choices": [{"message": {"content": "OK"}}]})

        cfg = Config(base_url="https://example.test/v1", embed_base_url="http://127.0.0.1:11434/v1",
                     llm_key="chat-secret", embed_key="", llm_model="deepseek", emb_model="bge-m3")
        with LlmClient(cfg, transport=httpx.MockTransport(respond)) as client:
            self.assertEqual(client.embed(["вопрос"]), [[0.6, 0.8]])
            self.assertEqual(client.chat([{"role": "user", "content": "привет"}]), "OK")
        self.assertEqual(seen, [
            ("http://127.0.0.1:11434/v1/embeddings", None),
            ("https://example.test/v1/chat/completions", "Bearer chat-secret"),
        ])


if __name__ == "__main__":
    unittest.main()
