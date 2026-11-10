"""Даты в тексте документов и вопросов."""
from __future__ import annotations

import re
from datetime import date

MONTHS = {
    "январ": 1, "феврал": 2, "март": 3, "апрел": 4, "ма": 5, "мая": 5, "май": 5, "мае": 5, "июн": 6,
    "июл": 7, "август": 8, "сентябр": 9, "октябр": 10, "ноябр": 11, "декабр": 12,
}
MONTH_RE = r"(январ[ьяе]|феврал[ьяе]|марте?а?|апрел[ьяе]|ма[йяе]|июн[ьяе]|июл[ьяе]|августе?а?|сентябр[ьяе]|октябр[ьяе]|ноябр[ьяе]|декабр[ьяе])"

NUMERIC_DATE = re.compile(r"\b(\d{1,2})\.(\d{1,2})\.(\d{4})\b")
TEXT_DATE = re.compile(r"\b(\d{1,2})\s+" + MONTH_RE + r"\s+(\d{4})", re.IGNORECASE)
MONTH_YEAR = re.compile(r"\b(?:в\s+|с\s+|до\s+|по\s+)?" + MONTH_RE + r"\s+(\d{4})", re.IGNORECASE)
YEAR_ONLY = re.compile(r"\b(?:в|во|за|на|с|до)\s+(20\d{2}|19\d{2})\s*(?:год[аеуы]?|г\.)", re.IGNORECASE)
YEAR_BARE = re.compile(r"\b(20[0-3]\d)\b")


def month_number(word: str) -> int | None:
    word = word.lower()
    for prefix in sorted(MONTHS, key=len, reverse=True):
        if word.startswith(prefix):
            return MONTHS[prefix]
    return None


def _safe(year: int, month: int, day: int) -> date | None:
    try:
        return date(year, month, day)
    except ValueError:
        return None


def find_dates(text: str) -> list[tuple[int, date]]:
    """Все полные даты в тексте с позицией: «01.03.2025» и «1 августа 2024 г.»."""
    found: list[tuple[int, date]] = []
    for match in NUMERIC_DATE.finditer(text):
        parsed = _safe(int(match.group(3)), int(match.group(2)), int(match.group(1)))
        if parsed:
            found.append((match.start(), parsed))
    for match in TEXT_DATE.finditer(text):
        month = month_number(match.group(2))
        parsed = _safe(int(match.group(3)), month, int(match.group(1))) if month else None
        if parsed:
            found.append((match.start(), parsed))
    return sorted(found, key=lambda item: item[0])


def first_date(text: str) -> date | None:
    dates = find_dates(text)
    return dates[0][1] if dates else None


def date_after(text: str, marker: str) -> date | None:
    """Первая дата после слова-маркера: «Действует с 01.03.2025», «утратила силу с ...»."""
    position = text.lower().find(marker.lower())
    if position < 0:
        return None
    for start, parsed in find_dates(text[position:position + 120]):
        return parsed
    return None


def event_period(question: str) -> tuple[date, date] | None:
    """Период события из вопроса: точная дата, «в августе 2024», «в 2024 году».

    Возвращает (начало, конец) периода. Даты внутри номеров договоров («П-17/2024») не считаются.
    """
    cleaned = re.sub(r"[А-ЯA-Z]{1,3}-\d+[/-]\d{4}", " ", question)
    # Год в названии редакции сам по себе не является датой события.
    cleaned = re.sub(r"редакци[ияею]\s+(?:от\s+)?\d{4}\s*(?:год[ауы]?)?",
                     " ", cleaned, flags=re.IGNORECASE)
    dates = find_dates(cleaned)
    if dates:
        day = dates[0][1]
        return day, day
    match = MONTH_YEAR.search(cleaned)
    if match:
        month = month_number(match.group(1))
        year = int(match.group(2))
        if month:
            end_month = date(year + (month == 12), month % 12 + 1, 1)
            return date(year, month, 1), date.fromordinal(end_month.toordinal() - 1)
    match = YEAR_ONLY.search(cleaned) or YEAR_BARE.search(cleaned)
    if match:
        year = int(match.group(1))
        return date(year, 1, 1), date(year, 12, 31)
    return None


def fmt(day: date | None) -> str:
    return day.strftime("%d.%m.%Y") if day else ""
