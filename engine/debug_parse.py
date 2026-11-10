"""Отладка разбора базы: python debug_parse.py <путь к базе> [doc_id]"""
import sys
from pathlib import Path

import kb_loader
import kb_parse

sys.stdout.reconfigure(encoding="utf-8")
docs = kb_parse.parse_all(kb_loader.load_knowledge_base(Path(sys.argv[1])))
only = sys.argv[2] if len(sys.argv) > 2 else ""
for doc in docs:
    if only and only not in doc.doc_id:
        continue
    print(f"== {doc.doc_id} [{doc.category}/{doc.doc_type}] «{doc.display_name}» number={doc.number} "
          f"date={doc.doc_date} parent={doc.parent_number} cp={doc.counterparty} ed={doc.edition_date} "
          f"from={doc.effective_from} to={doc.effective_to} group={doc.group_key} exported={doc.exported_at}")
    if doc.removed_lines:
        print("   REMOVED:", doc.removed_lines)
    if doc.amendments:
        print("   AMEND:", [(str(d), n[:60]) for d, n in doc.amendments[:4]], len(doc.amendments))
    for chunk in doc.chunks[: (999 if only else 6)]:
        print(f"   - [{chunk.path}] sec={chunk.section!r} cl={chunk.clause} sub={chunk.subclause} "
              f"len={len(chunk.text)} notes={len(chunk.notes)} :: {chunk.text[:90]!r}")
    print(f"   chunks={len(doc.chunks)}")
