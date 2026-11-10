"""Отладка ответа движка против фейковой модели: python debug_answer.py <база> <questions.csv> <id> [--prompt]"""
import csv
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import kb
from config import Config
from index import SearchIndex
from llm_client import LlmClient
from pipeline import Engine
from tests.fake_openai import FakeOpenAI

sys.stdout.reconfigure(encoding="utf-8")
base = kb.load(Path(sys.argv[1]))
with open(sys.argv[2], encoding="utf-8-sig", newline="") as f:
    rows = {row["id"]: row["question"] for row in csv.DictReader(f)}
with FakeOpenAI() as fake:
    cfg = Config(base_url=fake.url, llm_model="fake", emb_model="fake-emb", reranker_model="fake-rr", use_reranker=True)
    with LlmClient(cfg) as client:
        index = SearchIndex(base)
        index.build_vectors(client)
        engine = Engine(base, index, client)
        for qid in sys.argv[3].split(","):
            result = engine.answer(rows[qid])
            print(f"#{qid} [{result.kind}/{result.mode}] {result.text}")
            print("   cites:", [(c.document, c.path) for c in result.citations], result.problems)
            if "--prompt" in sys.argv:
                print(fake.last_messages[1]["content"])
