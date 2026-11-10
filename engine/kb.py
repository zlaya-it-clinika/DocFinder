"""База знаний целиком: документы, фрагменты и связи между ними.

Связи:
- редакции одного документа (группа по названию): какая действовала на дату;
- дополнительные соглашения → договор, какие пункты договора они меняют;
- документ, который ссылается на другой («в дополнение к п. 5.4 Регламента ...»).
"""
from __future__ import annotations

import re
from dataclasses import dataclass, field
from datetime import date
from pathlib import Path

import kb_loader
import kb_parse
import textnorm
from kb_parse import COMPANY, LAW, Chunk, Document

CLAUSE_REF = re.compile(r"(?:пункт\w*|п\.)\s*(\d+(?:\.\d+)+)", re.IGNORECASE)


@dataclass
class KnowledgeBase:
    docs: list[Document]
    chunks: list[Chunk] = field(default_factory=list)
    by_id: dict[str, Document] = field(default_factory=dict)
    by_number: dict[str, Document] = field(default_factory=dict)
    own_org: str = ""
    # doc_id ДС -> номера пунктов договора, которые оно меняет
    amended_clauses: dict[str, list[str]] = field(default_factory=dict)

    def doc(self, chunk: Chunk) -> Document:
        return self.by_id[chunk.doc_id]

    def edition_on(self, group_key: str, day: date | None) -> Document | None:
        """Редакция документа группы, действовавшая на дату; без даты - действующая."""
        editions = [doc for doc in self.docs if doc.group_key == group_key]
        if not editions:
            return None
        if day is None:
            current = [doc for doc in editions if doc.effective_to is None]
            return max(current or editions, key=lambda doc: doc.effective_from or date.min)
        valid = [doc for doc in editions
                 if (doc.effective_from is None or doc.effective_from <= day)
                 and (doc.effective_to is None or day < doc.effective_to)]
        if valid:
            return max(valid, key=lambda doc: doc.effective_from or date.min)
        return None

    def is_current(self, doc: Document) -> bool:
        return doc.effective_to is None

    def neighbors(self, chunk: Chunk, radius: int = 1) -> list[Chunk]:
        doc = self.doc(chunk)
        found = []
        for other in doc.chunks:
            if other is chunk or abs(other.order - chunk.order) > radius:
                continue
            if chunk.section and other.section != chunk.section:
                continue
            found.append(other)
        return found

    def summary(self) -> str:
        laws = sum(doc.category == LAW for doc in self.docs)
        return f"документов {len(self.docs)} (законов {laws}), фрагментов {len(self.chunks)}"


def build(docs: list[Document]) -> KnowledgeBase:
    kb = KnowledgeBase(docs=docs, own_org=kb_parse.own_organization(docs))
    for doc in docs:
        kb.by_id[doc.doc_id] = doc
        if doc.number and doc.doc_type in ("contract", "memo", "letter", "order"):
            kb.by_number.setdefault(doc.number, doc)
    for doc in docs:
        for chunk in doc.chunks:
            chunk.index = len(kb.chunks)
            kb.chunks.append(chunk)
    _link_supplements(kb)
    _link_editions(kb)
    _link_references(kb)
    return kb


def _link_supplements(kb: KnowledgeBase) -> None:
    for doc in kb.docs:
        if doc.doc_type != "supplement" or not doc.parent_number:
            continue
        parent = kb.by_number.get(doc.parent_number)
        if parent is None:
            continue
        doc.parent_id = parent.doc_id
        parent.supplements.append(doc.doc_id)
        if not doc.counterparty:
            doc.counterparty = parent.counterparty
        # «Изложить пункт 3.2 договора в следующей редакции» - пункт, который меняет ДС
        kb.amended_clauses[doc.doc_id] = sorted(set(CLAUSE_REF.findall(doc.text)) - {doc.number})


def _link_editions(kb: KnowledgeBase) -> None:
    groups: dict[str, list[Document]] = {}
    for doc in kb.docs:
        if doc.group_key:
            groups.setdefault(doc.group_key, []).append(doc)
    for editions in groups.values():
        ordered = sorted(editions, key=lambda doc: doc.effective_from or date.min)
        ids = [doc.doc_id for doc in ordered]
        for position, doc in enumerate(ordered):
            doc.editions = ids
            # Редакция без даты окончания, после которой есть более новая, действовала до начала следующей
            if doc.effective_to is None and position + 1 < len(ordered):
                doc.effective_to = ordered[position + 1].effective_from


def _link_references(kb: KnowledgeBase) -> None:
    """Документ упоминает другой по названию: письмо - регламент, записка - договоры и т. п."""
    for doc in kb.docs:
        if doc.category != COMPANY:
            continue
        low = doc.text.lower()
        for other in kb.docs:
            if other is doc or other.category != COMPANY:
                continue
            mentioned = False
            if other.number and other.number.lower() in low and other.doc_type == "contract":
                mentioned = True
            elif other.group_key and other.doc_type == "regulation":
                name = other.title.lower().split(" ооо")[0]
                words = [textnorm.stem(word) for word in re.findall(r"[а-яё]+", name)]
                pattern = r"\s+".join(re.escape(word) + r"[а-яё]*" for word in words)
                mentioned = bool(words) and re.search(pattern, low) is not None
                # Ссылка на конкретную редакцию: берём только её
                if mentioned and other.edition_date:
                    marker = other.edition_date.strftime("%d.%m.%Y")
                    if "ред." in low and marker not in low and any(
                            d.edition_date and d.edition_date.strftime("%d.%m.%Y") in low
                            for d in kb.docs if d.group_key == other.group_key):
                        mentioned = False
            if mentioned and other.doc_id not in doc.related and doc.doc_id not in other.editions:
                doc.related.append(other.doc_id)
                if doc.doc_id not in other.related:
                    other.related.append(doc.doc_id)


def load(path: Path) -> KnowledgeBase:
    return build(kb_parse.parse_all(kb_loader.load_knowledge_base(path)))
