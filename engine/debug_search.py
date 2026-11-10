"""Отладка: сущности и BM25-поиск по демо-вопросам. python debug_search.py <база> <questions.csv>"""
import csv
import sys
from pathlib import Path

import entities
import kb
from dialog import parse_dialog
from index import SearchIndex

sys.stdout.reconfigure(encoding="utf-8")
base = kb.load(Path(sys.argv[1]))
index = SearchIndex(base)
with open(sys.argv[2], encoding="utf-8-sig", newline="") as f:
    rows = list(csv.DictReader(f))
for row in rows:
    dialog = parse_dialog(row["question"])
    last = entities.extract(base, dialog.last_user)
    ctx = entities.extract(base, "\n".join(t.text for t in dialog.history))
    ent = entities.merge(last, ctx)
    print(f"#{row['id']} turns={len(dialog.turns)} last={dialog.last_user[:70]!r}")
    print(f"   docs={ent.doc_ids} amb={ent.ambiguous} period={ent.period} unknown={ent.unknown_numbers}")
    hits = index.search(dialog.last_user + "\n" + "\n".join(dialog.user_texts()[:-1]), limit=5)
    for i, score in hits:
        chunk = base.chunks[i]
        print(f"   {score:.4f} {base.doc(chunk).display_name} [{chunk.path}] {chunk.text[:60]!r}")
