"""Сравнение BM25 и BGE-M3 на локальном Ollama без вызова модели ответа.

Пример:
    python eval_embeddings.py --kb tests/data/demo/knowledge-base.zip \
        --cases tests/challenge_cases.csv --cache dist/bge-m3-vectors.npz
"""
from __future__ import annotations

import argparse
import json
from pathlib import Path

import kb
import retrieval
from config import Config
from dialog import parse_dialog
from eval_local import read_csv
from index import SearchIndex
from llm_client import LlmClient


def expected_rank(case: dict[str, str], result: retrieval.Retrieved) -> int | None:
    document = case.get("expected_doc", "").lower()
    path = case.get("expected_path", "")
    if not document and not path:
        return 1
    return next((rank for rank, item in enumerate(result.items, 1)
                 if (not document or document in item.doc.display_name.lower())
                 and (not path or path == item.chunk.path)), None)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--kb", type=Path, required=True)
    parser.add_argument("--cases", type=Path, required=True)
    parser.add_argument("--cache", type=Path, required=True)
    parser.add_argument("--url", default="http://127.0.0.1:11434/v1")
    parser.add_argument("--model", default="bge-m3")
    args = parser.parse_args()

    cases = [case for case in read_csv(args.cases)
             if case.get("expected_doc") or case.get("expected_path")]
    base = kb.load(args.kb)
    index = SearchIndex(base)
    cfg = Config(embed_base_url=args.url.rstrip("/"), emb_model=args.model)
    with LlmClient(cfg) as client:
        if not index.build_vectors(client, cache_path=args.cache):
            raise SystemExit("Не удалось построить векторы через Ollama")
        vectors = client.embed([case["question"] for case in cases])
        bm_only = {}
        hybrid = {}
        for case, vector in zip(cases, vectors):
            dialog = parse_dialog(case["question"])
            bm_only[case["id"]] = expected_rank(case, retrieval.retrieve(base, index, dialog))
            hybrid[case["id"]] = expected_rank(case, retrieval.retrieve(base, index, dialog, query_vector=vector))
    ids = {case["id"] for case in cases}
    def count_within(ranks, limit):
        return sum(rank is not None and rank <= limit for rank in ranks.values())
    result = {
        "model": args.model,
        "cases": len(cases),
        "bm25_found": count_within(bm_only, 999),
        "hybrid_found": count_within(hybrid, 999),
        "bm25_at_1": count_within(bm_only, 1),
        "hybrid_at_1": count_within(hybrid, 1),
        "bm25_at_3": count_within(bm_only, 3),
        "hybrid_at_3": count_within(hybrid, 3),
        "improved_rank": sorted(case_id for case_id in ids
                                if hybrid[case_id] is not None
                                and (bm_only[case_id] is None or hybrid[case_id] < bm_only[case_id])),
        "worse_rank": sorted(case_id for case_id in ids
                             if bm_only[case_id] is not None
                             and (hybrid[case_id] is None or hybrid[case_id] > bm_only[case_id])),
        "hybrid_missing": sorted(case_id for case_id in ids if hybrid[case_id] is None),
    }
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
