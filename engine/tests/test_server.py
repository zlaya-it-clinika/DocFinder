"""Сервис движка: /health, /answer, /documents, /structure, /reindex, авторизация по токену."""
import json
import tempfile
import threading
import unittest
import urllib.error
import urllib.request
from pathlib import Path

from config import Config
from server import EngineService, create_server
from tests.common import SOURCE_KB, ensure_demo_data
from tests.fake_openai import FakeOpenAI

TOKEN = "test-token"


class ServerApi(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.fake = FakeOpenAI().__enter__()
        cls.cache = tempfile.TemporaryDirectory()
        kb_path = SOURCE_KB if SOURCE_KB.is_dir() else ensure_demo_data() / "knowledge-base.zip"
        cfg = Config(base_url=cls.fake.url, llm_model="fake", emb_model="fake-emb")
        cls.service = EngineService(cfg, kb_path, Path(cls.cache.name))
        cls.service.load_from_path()
        cls.server = create_server(cls.service, "127.0.0.1", 0, TOKEN)
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()
        cls.url = f"http://127.0.0.1:{cls.server.server_address[1]}"

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()
        cls.fake.__exit__()
        cls.cache.cleanup()

    def call(self, method: str, path: str, body: dict | None = None, token: str | None = TOKEN):
        data = json.dumps(body).encode("utf-8") if body is not None else None
        request = urllib.request.Request(self.url + path, data=data, method=method)
        request.add_header("Content-Type", "application/json")
        if token:
            request.add_header("Authorization", "Bearer " + token)
        try:
            with urllib.request.urlopen(request, timeout=30) as response:
                return response.status, json.loads(response.read().decode("utf-8"))
        except urllib.error.HTTPError as error:
            return error.code, json.loads(error.read().decode("utf-8"))

    def test_health_without_token(self):
        status, body = self.call("GET", "/health", token=None)
        self.assertEqual(status, 200)
        self.assertEqual(body["documents"], 21)

    def test_token_required(self):
        status, _ = self.call("POST", "/answer", {"question": "Какой срок испытания?"}, token=None)
        self.assertEqual(status, 401)
        status, _ = self.call("POST", "/answer", {"question": "Какой срок испытания?"}, token="wrong")
        self.assertEqual(status, 401)

    def test_answer_with_history(self):
        status, body = self.call("POST", "/answer", {
            "question": "А за сколько дней после поставки нужно проверить качество?",
            "history": [{"role": "user", "text": "Какой гарантийный срок по договору с ООО «Альфа-Трейд»?"},
                        {"role": "assistant", "text": "24 месяца с даты поставки (п. 5.2)."}],
        })
        self.assertEqual(status, 200, body)
        self.assertEqual(body["mode"], "llm")
        self.assertTrue(body["text"])
        self.assertTrue(body["citations"])
        self.assertIn("doc_id", body["citations"][0])
        self.assertEqual(self.service.vector_model, "fake-emb")

    def test_settings_override_without_model(self):
        status, body = self.call("POST", "/answer", {"question": "Какой срок испытания главного бухгалтера?",
                                                     "settings": {"llm_model": "", "base_url": "http://127.0.0.1:9"}})
        self.assertEqual(status, 200, body)
        self.assertTrue(body["text"])

    def test_bad_request(self):
        status, _ = self.call("POST", "/answer", {"question": ""})
        self.assertEqual(status, 400)

    def test_documents_and_structure(self):
        status, body = self.call("GET", "/documents")
        self.assertEqual(status, 200)
        contract = next(doc for doc in body["documents"] if doc["number"] == "А-08/2024")
        status, structure = self.call("GET", "/documents/" + urllib.request.quote(contract["doc_id"]) + "/structure")
        self.assertEqual(status, 200)
        self.assertTrue(any(chunk["path"] == "п. 4.3" for chunk in structure["chunks"]))
        status, _ = self.call("GET", "/documents/nope/structure")
        self.assertEqual(status, 404)

    def test_reindex_with_documents(self):
        service = EngineService(Config(), None, Path(self.cache.name))
        service.load_from_documents([{"file_name": "законы__ТК_ст70.txt",
                                      "text": "Статья 70. Испытание\nСрок испытания не может превышать трех месяцев."}])
        self.assertEqual(len(service.kb.docs), 1)
        self.assertEqual(service.kb.docs[0].category, "law")


if __name__ == "__main__":
    unittest.main()
