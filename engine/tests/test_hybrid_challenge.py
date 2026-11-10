"""Гибридный поиск сохраняет ключевые положения при неточном реранжировании."""
import csv
import unittest
from pathlib import Path

import retrieval
from config import Config
from dialog import parse_dialog
from index import SearchIndex
from kb import load
from llm_client import LlmClient
from tests.common import ensure_demo_data
from tests.fake_openai import FakeOpenAI


class HybridChallenge(unittest.TestCase):
    def test_lexical_evidence_survives_reranking(self):
        case_path = Path(__file__).with_name("challenge_cases.csv")
        selected = {"x012", "x029", "x057", "x059", "x069", "x107"}
        with case_path.open(encoding="utf-8", newline="") as file:
            cases = [row for row in csv.DictReader(file) if row["id"] in selected]

        kb = load(ensure_demo_data() / "knowledge-base.zip")
        with FakeOpenAI() as fake:
            config = Config(base_url=fake.url, llm_model="fake", emb_model="fake-emb",
                            reranker_model="fake-rerank")
            with LlmClient(config) as client:
                index = SearchIndex(kb)
                self.assertTrue(index.build_vectors(client))
                vectors = client.embed([case["question"] for case in cases])
                for case, vector in zip(cases, vectors):
                    with self.subTest(case=case["id"]):
                        found = retrieval.retrieve(kb, index, parse_dialog(case["question"]),
                                                   client, vector)
                        self.assertTrue(any(
                            case["expected_doc"].lower() in item.doc.display_name.lower()
                            and item.chunk.path == case["expected_path"]
                            for item in found.items
                        ))


if __name__ == "__main__":
    unittest.main()
