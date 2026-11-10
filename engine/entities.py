"""Сущности вопроса: какие документы компании имеются в виду.

Договор узнаётся по номеру («А-08/2024», «П-17-2024») или по контрагенту.
Контрагенты сравниваются по основам слов целиком, а не подстрокой: «Альфа-Трейд»
не должно совпадать с «Альфа-Трейдинг». Если названо только общее слово («Альфа»)
и подходят несколько контрагентов - это неоднозначность, нужен уточняющий вопрос.
"""
from __future__ import annotations

import re
from dataclasses import dataclass, field
from datetime import date

import dates
import textnorm
from kb import KnowledgeBase
from kb_parse import Document, normalize_number

NUMBER_IN_TEXT = re.compile(r"\b([A-Za-zА-Яа-яЁё]{1,4})\s?-?\s?(\d{1,4})\s?[/-]\s?(\d{4})\b")
GENERIC_WORDS = {"ооо", "ао", "пао", "компания", "фирма", "общество", "групп", "group", "трейд", "склад"}


@dataclass
class Entities:
    doc_ids: list[str] = field(default_factory=list)       # точно названные документы
    ambiguous: list[list[str]] = field(default_factory=list)  # группы кандидатов при неоднозначности
    period: tuple[date, date] | None = None
    numbers: list[str] = field(default_factory=list)
    unknown_numbers: list[str] = field(default_factory=list)  # номер назван, но в базе такого нет


def _name_tokens(name: str) -> list[str]:
    parts = re.findall(r"[a-zа-яё]+", name.lower().replace("ё", "е"))
    return [textnorm.stem(part) for part in parts if part]


def _phrase_in(tokens: list[str], text_tokens: list[str]) -> bool:
    size = len(tokens)
    return size > 0 and any(text_tokens[i:i + size] == tokens for i in range(len(text_tokens) - size + 1))


def _text_tokens(text: str) -> list[str]:
    return [textnorm.stem(part) for part in re.findall(r"[a-zа-яё]+", text.lower().replace("ё", "е"))]


def find_numbers(text: str) -> list[str]:
    found = []
    for match in NUMBER_IN_TEXT.finditer(text):
        number = normalize_number(f"{match.group(1)}-{match.group(2)}/{match.group(3)}")
        if number not in found:
            found.append(number)
    return found


def mentions_document_number(text: str, number: str) -> bool:
    """Короткие номера писем и записок: «К-12», «ЮО-7» (без обязательного года)."""
    normalized = text.lower().replace("ё", "е").replace("–", "-").replace("—", "-")
    parts = [re.escape(part.lower().replace("ё", "е")) for part in re.split(r"[-/]", number) if part]
    if len(parts) < 2:
        return False
    pattern = r"(?<![a-zа-яё0-9])" + r"\s*[-/]\s*".join(parts) + r"(?![a-zа-яё0-9])"
    return re.search(pattern, normalized, re.IGNORECASE) is not None


def counterparty_docs(kb: KnowledgeBase) -> dict[str, list[Document]]:
    """Контрагент -> его договоры (без своей организации)."""
    result: dict[str, list[Document]] = {}
    for doc in kb.docs:
        if doc.counterparty and doc.doc_type == "contract":
            result.setdefault(doc.counterparty, []).append(doc)
    return result


def extract(kb: KnowledgeBase, text: str) -> Entities:
    entities = Entities(period=dates.event_period(text))
    for number in find_numbers(text):
        entities.numbers.append(number)
        doc = kb.by_number.get(number)
        if doc:
            _add(entities, doc.doc_id)
        else:
            entities.unknown_numbers.append(number)
    for doc in kb.docs:
        if doc.number and doc.doc_type in ("memo", "letter", "order") and mentions_document_number(text, doc.number):
            _add(entities, doc.doc_id)
    text_tokens = _text_tokens(text)
    parties = counterparty_docs(kb)
    exact: list[str] = []
    for name in parties:
        if _phrase_in(_name_tokens(name), text_tokens):
            exact.append(name)
    # «Альфа-Трейдинг» в тексте даёт совпадение и для «Альфа-Трейдинг», и (по основам) не для «Альфа-Трейд»;
    # если всё же совпали оба - оставляем более длинное название
    exact = [name for name in exact if not any(other != name and name.lower() in other.lower() for other in exact)]
    for name in exact:
        for doc in parties[name]:
            _add(entities, doc.doc_id)
    if not exact and not entities.doc_ids:
        # Первое слово названия («Альфа») подходит нескольким контрагентам - неоднозначно
        by_first: dict[str, list[str]] = {}
        for name in parties:
            tokens = _name_tokens(name)
            if tokens and tokens[0] not in GENERIC_WORDS and tokens[0] in text_tokens:
                by_first.setdefault(tokens[0], []).append(name)
        for names in by_first.values():
            ids = [doc.doc_id for name in names for doc in parties[name]]
            if len(names) > 1:
                entities.ambiguous.append(ids)
            else:
                for doc_id in ids:
                    _add(entities, doc_id)
    return entities


def _add(entities: Entities, doc_id: str) -> None:
    if doc_id not in entities.doc_ids:
        entities.doc_ids.append(doc_id)


def merge(primary: Entities, context: Entities) -> Entities:
    """Сущности последней реплики дополняются сущностями из истории диалога."""
    merged = Entities(doc_ids=list(primary.doc_ids), ambiguous=list(primary.ambiguous),
                      period=primary.period or context.period, numbers=list(primary.numbers),
                      unknown_numbers=list(primary.unknown_numbers))
    if not merged.doc_ids:
        if primary.ambiguous:
            # Неоднозначность в последней реплике, а в истории конкретный документ из той же группы - берём его
            chosen = [doc_id for doc_id in context.doc_ids if any(doc_id in group for group in primary.ambiguous)]
            if chosen:
                merged.doc_ids, merged.ambiguous = chosen, []
        else:
            merged.doc_ids = list(context.doc_ids)
            if not context.doc_ids:
                merged.ambiguous = list(context.ambiguous)
    return merged
