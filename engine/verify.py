"""Разбор ответа модели и проверка: нет ли чисел и ссылок, которых нет во фрагментах.

Выдуманный факт обнуляет оценку судьи, поэтому проверка строгая:
- каждое число ответа есть в использованных фрагментах, в сведениях о документах или в вопросе;
- каждая ссылка [Fn] существует;
- номера пунктов/статей («п. 3.1», «ст. 70») есть среди путей использованных фрагментов или в их тексте.
Логика сверки чисел перенесена из core/internal/grounding/numbers.go и расширена.
"""
from __future__ import annotations

import re
from dataclasses import dataclass, field

KINDS = ("answer", "partial", "refusal", "correction", "clarify", "illegal", "out_of_scope")
TAG_RE = re.compile(r"^\s*\**\s*(KIND|ANSWER|MISSING|USED)\s*\**\s*[:：]\s*", re.IGNORECASE | re.MULTILINE)
LABEL_RE = re.compile(r"\[\s*(F\d+(?:\s*[,;]\s*F?\d+)*)\s*\]", re.IGNORECASE)

# Число: «450 000», «8 400 000», «0,1», «27 093». Пробелы-разделители тысяч склеиваются
NUMBER_RE = re.compile(r"(?<![\w.,])\d{1,3}(?:[ \u00a0\u202f]\d{3})+(?!\d)|(?<!\w)\d+(?:[.,]\d+)*")
LIST_MARKER = re.compile(r"(?m)(^|[;:]\s|\s)\d{1,2}\)\s")
REF_RE = re.compile(r"\b(п|пп|ч|ст|разд|подп)\.\s*(\d+(?:\.\d+)*)", re.IGNORECASE)
SCALE_RE = re.compile(r"(\d+(?:[.,]\d+)?)\s*(тыс|тысяч|млн|миллион\w*|млрд)")
SCALES = (("млрд", 1_000_000_000), ("млн", 1_000_000), ("миллион", 1_000_000), ("тыс", 1_000))

NUMBER_WORDS = {
    "один": "1", "одна": "1", "одно": "1", "одного": "1", "одной": "1", "одному": "1", "одним": "1", "одну": "1",
    "два": "2", "две": "2", "двух": "2", "двум": "2", "двумя": "2",
    "три": "3", "трех": "3", "трем": "3", "тремя": "3",
    "четыре": "4", "четырех": "4", "четырем": "4", "четырьмя": "4",
    "пять": "5", "пяти": "5", "пятью": "5", "шесть": "6", "шести": "6", "семь": "7", "семи": "7",
    "восемь": "8", "восьми": "8", "девять": "9", "девяти": "9", "десять": "10", "десяти": "10",
    "двенадцать": "12", "двенадцати": "12", "пятнадцать": "15", "пятнадцати": "15",
    "двадцать": "20", "двадцати": "20", "тридцать": "30", "тридцати": "30", "сорок": "40", "сорока": "40",
    "шестьдесят": "60", "шестидесяти": "60", "девяносто": "90", "девяноста": "90", "сто": "100",
    "первые": "1", "первых": "1", "полуторном": "1.5", "двойном": "2",
}
# Числа, которые не несут фактов: «1» (один из вариантов), «0»
HARMLESS = {"1", "0"}


@dataclass
class ModelAnswer:
    kind: str = "answer"
    text: str = ""
    missing: str = ""
    used: list[str] = field(default_factory=list)
    parsed: bool = False


def parse_answer(raw: str) -> ModelAnswer:
    parts: dict[str, str] = {}
    matches = list(TAG_RE.finditer(raw))
    for position, match in enumerate(matches):
        end = matches[position + 1].start() if position + 1 < len(matches) else len(raw)
        parts[match.group(1).upper()] = raw[match.end():end].strip()
    if "ANSWER" not in parts:
        # Модель ответила без формата: весь текст - ответ
        return ModelAnswer(text=raw.strip(), used=labels_in(raw), parsed=False)
    kind_raw = parts.get("KIND", "").strip().lower()
    kind = kind_raw.split()[0].strip(".,;") if kind_raw else "answer"
    kind = kind if kind in KINDS else "answer"
    missing = parts.get("MISSING", "").strip()
    if missing.strip(".").lower() in ("-", "—", "нет", "ничего", "none", ""):
        missing = ""
    used = labels_in(parts.get("USED", "")) or labels_in(parts["ANSWER"])
    return ModelAnswer(kind=kind, text=parts["ANSWER"].strip(), missing=missing, used=used, parsed=True)


def labels_in(text: str) -> list[str]:
    labels: list[str] = []
    for number in re.findall(r"F\s*(\d+)", text, re.IGNORECASE):
        label = f"F{int(number)}"
        if label not in labels:
            labels.append(label)
    return labels


def normalize_number(token: str) -> str:
    value = re.sub(r"[ \u00a0\u202f]", "", token).replace(",", ".").strip(".")
    if re.fullmatch(r"\d+\.0+", value):
        value = value.split(".")[0]
    if re.fullmatch(r"\d+", value):
        value = value.lstrip("0") or "0"
    return value


def _scaled(text: str) -> list[tuple[str, str]]:
    """«12 млн» -> ("12", "12000000")."""
    found = []
    for match in SCALE_RE.finditer(text.lower()):
        multiplier = next(value for prefix, value in SCALES if match.group(2).startswith(prefix))
        base = float(match.group(1).replace(",", "."))
        found.append((normalize_number(match.group(1)), normalize_number(str(int(round(base * multiplier))))))
    return found


def numbers_in(text: str) -> set[str]:
    cleaned = LABEL_RE.sub(" ", LIST_MARKER.sub(" ", text))
    found = {normalize_number(token) for token in NUMBER_RE.findall(cleaned)}
    for base, full in _scaled(cleaned):
        found.discard(base)
        found.add(full)
    return found


def source_numbers(sources: list[str]) -> set[str]:
    known: set[str] = set()
    for source in sources:
        known.update(normalize_number(token) for token in NUMBER_RE.findall(source))
        known.update(full for _, full in _scaled(source))
        for word in re.findall(r"[а-яё]+", source.lower().replace("ё", "е")):
            if word in NUMBER_WORDS:
                known.add(NUMBER_WORDS[word])
        # Номера документов «А-08/2024»: 8 и 2024
        for left, right in re.findall(r"[A-ZА-ЯЁ]{1,4}-(\d{1,4})[/-](\d{4})", source):
            known.update({normalize_number(left), right})
    # «3.2» подтверждает и первую часть «3» (пункт 3)
    known.update(value.split(".")[0] for value in list(known) if "." in value)
    return known


def unsupported_numbers(answer: str, sources: list[str]) -> list[str]:
    known = source_numbers(sources)
    return sorted(number for number in numbers_in(answer) if number not in known and number not in HARMLESS)


def unsupported_refs(answer: str, used_paths: list[str], used_texts: list[str]) -> list[str]:
    """Ссылки «п. 3.1» / «ст. 70», которых нет среди путей и текста использованных фрагментов."""
    allowed: set[str] = set()
    for source in used_paths + used_texts:
        allowed.update(re.findall(r"\d+(?:\.\d+)*", source))
    bad = []
    for kind, number in REF_RE.findall(answer):
        ref = f"{kind}. {number}"
        if number not in allowed and ref not in bad:
            bad.append(ref)
    return bad


def unknown_labels(answer: str, valid: set[str]) -> list[str]:
    labels = labels_in(" ".join(LABEL_RE.findall(answer)))
    return [label for label in labels if label not in valid]
