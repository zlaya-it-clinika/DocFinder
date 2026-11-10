"""Отладка ответа против реального API (ключ только из окружения):
    python debug_live.py <база> <questions.csv> <id,id> [--prompt]
Показывает найденные фрагменты, сырой ответ модели, найденные проверкой проблемы и итоговый текст."""
import csv
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import kb
import pipeline
import prompts
import verify
from config import load_config
from dialog import parse_dialog
from index import SearchIndex
from llm_client import LlmClient
from pipeline import Engine

sys.stdout.reconfigure(encoding="utf-8")
base = kb.load(Path(sys.argv[1]))
with open(sys.argv[2], encoding="utf-8-sig", newline="") as f:
    rows = {row["id"]: row["question"] for row in csv.DictReader(f)}
cfg = load_config()
with LlmClient(cfg) as client:
    index = SearchIndex(base)
    index.build_vectors(client)
    engine = Engine(base, index, client if cfg.llm_ready else None, top_n=cfg.reranker_top_n)
    original_chat = client.chat
    raw_log = []

    def logged_chat(messages, **kwargs):
        raw = original_chat(messages, **kwargs)
        raw_log.append(raw)
        return raw

    client.chat = logged_chat
    for qid in sys.argv[3].split(","):
        raw_log.clear()
        dialog = parse_dialog(rows[qid])
        import retrieval
        retrieved = retrieval.retrieve(base, index, dialog, client, None, cfg.reranker_top_n, None)
        print(f"=== #{qid} {rows[qid][:120]}")
        print(f"coverage={retrieved.coverage:.2f} entities={retrieved.entities.doc_ids}")
        for item in retrieved.items:
            print(f"  [{item.label}] {item.doc.display_name} | {item.chunk.path} | {item.chunk.text[:90]!r}")
        result = engine.answer(rows[qid])
        for number, raw in enumerate(raw_log, 1):
            print(f"--- RAW #{number}:\n{raw}")
            parsed = verify.parse_answer(raw)
            print("   problems:", pipeline.check(parsed, retrieved, dialog.last_user))
        print(f"--- FINAL [{result.kind}/{result.mode}] {result.text}")
        if "--prompt" in sys.argv:
            print(prompts.build_messages(base.own_org, dialog.last_user, dialog.context_text(), retrieved, False)[1]["content"])
