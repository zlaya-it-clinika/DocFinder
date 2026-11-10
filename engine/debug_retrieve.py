"""Отладка отбора контекста: python debug_retrieve.py <база> <questions.csv> [id]"""
import csv
import sys
from pathlib import Path

import kb
import retrieval
from dialog import parse_dialog
from index import SearchIndex

sys.stdout.reconfigure(encoding="utf-8")
base = kb.load(Path(sys.argv[1]))
index = SearchIndex(base)
only = sys.argv[3] if len(sys.argv) > 3 else ""
with open(sys.argv[2], encoding="utf-8-sig", newline="") as f:
    rows = list(csv.DictReader(f))
for row in rows:
    if only and row["id"] != only:
        continue
    result = retrieval.retrieve(base, index, parse_dialog(row["question"]))
    chars = sum(len(item.chunk.text) for item in result.items)
    print(f"#{row['id']} items={len(result.items)} chars={chars} cov={result.coverage:.2f} docs={result.entities.doc_ids}")
    for fact in result.facts:
        print("   FACT:", fact)
    for warning in result.warnings:
        print("   WARN:", warning)
    for item in result.items[: (100 if only else 12)]:
        print(f"   {item.label} {item.doc.display_name} [{item.chunk.path}] {item.marks} {item.chunk.text[:50]!r}")
