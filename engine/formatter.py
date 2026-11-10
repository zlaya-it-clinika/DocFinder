"""Финальный текст ответа: ссылки [Fn] -> человекочитаемые, без markdown, в одну строку для CSV."""
from __future__ import annotations

import re

from retrieval import ContextItem
from verify import LABEL_RE, labels_in

MAX_ANSWER_CHARS = 10000


def short_path(item: ContextItem) -> str:
    return item.chunk.path if item.chunk.path and item.chunk.path != "преамбула" else ""


def citation_text(items: list[ContextItem], answer_low: str) -> str:
    """«п. 3.1, п. 3.2» для одного документа; документ называется, если в тексте ответа его нет."""
    groups: dict[str, list[ContextItem]] = {}
    for item in items:
        groups.setdefault(item.doc.doc_id, []).append(item)
    parts = []
    for group in groups.values():
        doc = group[0].doc
        paths = list(dict.fromkeys(path for path in (short_path(item) for item in group) if path))
        named = _mentioned(doc, answer_low)
        if doc.category == "law" and paths:
            # «ст. 70, ч. 3»: статья уже в пути, документ - кодекс/закон
            label = paths[0] if named else f"{doc.act}, {paths[0]}"
            rest = [p.split(", ", 1)[1] if p.startswith(paths[0].split(",")[0] + ",") and "," in p else p
                    for p in paths[1:]]
            parts.append(", ".join([label] + rest))
        elif paths:
            prefix = "" if named else doc.display_name + ", "
            parts.append(prefix + ", ".join(paths))
        else:
            parts.append(doc.display_name)
    return "; ".join(parts)


def _mentioned(doc, answer_low: str) -> bool:
    if doc.category == "law":
        return doc.act.lower() in answer_low or (doc.article and f"ст. {doc.article}" in answer_low)
    if doc.number and doc.number.lower() in answer_low:
        return True
    return doc.display_name.lower() in answer_low


def replace_labels(text: str, by_label: dict[str, ContextItem]) -> str:
    low = text.lower()

    def substitute(match: re.Match) -> str:
        items = [by_label[label] for label in labels_in(match.group(0)) if label in by_label]
        if not items:
            return ""
        # Ссылка уже написана словами прямо перед меткой - метку убираем
        before = text[max(0, match.start() - 80):match.start()].lower()
        if all(short_path(item) and short_path(item).lower() in before for item in items):
            return ""
        return "(" + citation_text(items, low) + ")"

    replaced = LABEL_RE.sub(substitute, text)
    # «(п. 3.1) (п. 3.1)» и пробелы перед знаками
    replaced = re.sub(r"\((?P<x>[^()]+)\)\s*\((?P=x)\)", r"(\g<x>)", replaced)
    replaced = re.sub(r"\(\s*\)", "", replaced)
    return re.sub(r"\s+([.,;:])", r"\1", replaced)


def clean_markdown(text: str) -> str:
    text = re.sub(r"\*\*|__|`", "", text)
    text = re.sub(r"(?m)^\s{0,3}#{1,6}\s*", "", text)
    text = re.sub(r"(?m)^\s*[-*•]\s+", "", text)
    return text


def to_single_line(text: str) -> str:
    lines = [line.strip() for line in text.split("\n") if line.strip()]
    joined = ""
    for line in lines:
        if joined and not re.search(r"[.!?:;»)]$", joined):
            joined += "."
        joined = (joined + " " + line).strip()
    return re.sub(r"\s{2,}", " ", joined)


LABELS = r"F\d+(?:\s*[,;]\s*F?\d+)*"
PAREN_LABEL_RE = re.compile(r"\(\s*(?:фрагмент\w*\s+)?(" + LABELS + r")\s*\)", re.IGNORECASE)
WORD_LABEL_RE = re.compile(r"\b(?:(?:в|во|по|см\.|согласно)\s+)?фрагмент\w*\s+(" + LABELS + r")", re.IGNORECASE)


TAIL_LABEL_RE = re.compile(r"\s*[,;]\s*" + LABELS + r"(?=\s*\))", re.IGNORECASE)


def normalize_labels(text: str) -> str:
    """Модель иногда пишет «(F4)», «(п. 3.1, F4)» или «фрагмент F1» вместо «[F4]»: приводим к одному виду."""
    # «(п. 3.1, F4)»: пункт уже назван словами, метка в хвосте скобки лишняя
    text = TAIL_LABEL_RE.sub("", text)
    text = PAREN_LABEL_RE.sub(r"[\1]", text)
    text = WORD_LABEL_RE.sub(r"[\1]", text)
    # «[F1][F2]» и «[F1] [F2]» -> «[F1, F2]»: одна ссылка на несколько пунктов, а не россыпь скобок
    return re.sub(r"\]\s*\[(?=F\d)", ", ", text)


SOURCES_TAIL_RE = re.compile(r"\s*(?:Источники?|Фрагменты?|Sources?)\s*:\s*" + LABELS + r"\s*\.?", re.IGNORECASE)
BARE_LABEL_RE = re.compile(r"\s*\bF\d+\b(?:\s*[,;]\s*F\d+\b)*", re.IGNORECASE)


def drop_leftover_labels(text: str) -> str:
    """Служебные метки не должны попасть в ответ: «Источники: F7, F8.» и одиночные «F3» убираем."""
    text = SOURCES_TAIL_RE.sub("", text)
    text = BARE_LABEL_RE.sub("", text)
    return re.sub(r"\s+([.,;:)])", r"\1", text)


def plain_wording(text: str) -> str:
    """Внутреннее слово «фрагмент» в ответе пользователю не нужно: говорим «в базе знаний»."""
    text = re.sub(r"(?i)\b(в|во)\s+(предоставленных|найденных|приведённых|приведенных|переданных)?\s*фрагментах"
                  r"(\s+базы\s+знаний)?", lambda m: "В базе знаний" if m.group(0)[0] == "В" else "в базе знаний", text)
    return re.sub(r"(?i)\bфрагмент(ы|ах|ов|ам)?\s+базы\s+знаний", "база знаний", text)


def finalize(text: str, by_label: dict[str, ContextItem]) -> str:
    result = to_single_line(clean_markdown(plain_wording(drop_leftover_labels(replace_labels(normalize_labels(text), by_label)))))
    if len(result) > MAX_ANSWER_CHARS:
        cut = result[:MAX_ANSWER_CHARS - 1]
        result = cut[:cut.rfind(" ")] + "…"
    return result
