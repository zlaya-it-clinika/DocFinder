"""Нормализация русского текста для поиска: токены, стемминг, числа.

Стеммер - компактная реализация Snowball (Porter) для русского языка без внешних зависимостей.
"""
from __future__ import annotations

import re
from functools import lru_cache

VOWELS = set("аеиоуыэюя")

PERFECTIVE_1 = ("вшись", "вши", "в")          # после а/я
PERFECTIVE_2 = ("ившись", "ывшись", "ивши", "ывши", "ив", "ыв")
REFLEXIVE = ("ся", "сь")
ADJECTIVE = ("ими", "ыми", "его", "ого", "ему", "ому", "ее", "ие", "ые", "ое", "ей", "ий", "ый", "ой",
             "ем", "им", "ым", "ом", "их", "ых", "ую", "юю", "ая", "яя", "ою", "ею")
PARTICIPLE_1 = ("ем", "нн", "вш", "ющ", "щ")  # после а/я
PARTICIPLE_2 = ("ивш", "ывш", "ующ")
VERB_1 = ("ла", "на", "ете", "йте", "ли", "й", "л", "ем", "н", "ло", "но", "ет", "ют", "ны", "ть", "ешь", "нно")
VERB_2 = ("ила", "ыла", "ена", "ейте", "уйте", "ите", "или", "ыли", "ей", "уй", "ил", "ыл", "им", "ым",
          "ен", "ило", "ыло", "ено", "ят", "ует", "уют", "ит", "ыт", "ены", "ить", "ыть", "ишь", "ую", "ю")
NOUN = ("иями", "ями", "ами", "ией", "иям", "ием", "иях", "ев", "ов", "ие", "ье", "еи", "ии", "ей", "ой",
        "ий", "ям", "ем", "ам", "ом", "ах", "ях", "ию", "ью", "ия", "ья", "а", "е", "и", "й", "о", "у",
        "ы", "ь", "ю", "я")
SUPERLATIVE = ("ейше", "ейш")
DERIVATIONAL = ("ость", "ост")


def _sorted(suffixes: tuple[str, ...]) -> tuple[str, ...]:
    return tuple(sorted(suffixes, key=len, reverse=True))


PERFECTIVE_1, PERFECTIVE_2, REFLEXIVE, ADJECTIVE = map(_sorted, (PERFECTIVE_1, PERFECTIVE_2, REFLEXIVE, ADJECTIVE))
PARTICIPLE_1, PARTICIPLE_2, VERB_1, VERB_2, NOUN = map(_sorted, (PARTICIPLE_1, PARTICIPLE_2, VERB_1, VERB_2, NOUN))


def _regions(word: str) -> tuple[int, int]:
    """Начало RV и R2 по правилам Snowball."""
    rv = len(word)
    for index, char in enumerate(word):
        if char in VOWELS:
            rv = index + 1
            break
    r1 = len(word)
    for index in range(1, len(word)):
        if word[index] not in VOWELS and word[index - 1] in VOWELS:
            r1 = index + 1
            break
    r2 = len(word)
    for index in range(r1 + 1, len(word)):
        if word[index] not in VOWELS and word[index - 1] in VOWELS:
            r2 = index + 1
            break
    return rv, r2


def _strip(word: str, rv: int, group1: tuple[str, ...], group2: tuple[str, ...] = ()) -> str | None:
    """Убрать самый длинный подходящий суффикс в зоне RV. group1 требует а/я перед суффиксом."""
    best: tuple[int, str] | None = None
    for suffix in group2:
        if word.endswith(suffix) and len(word) - len(suffix) >= rv:
            best = max(best or (0, ""), (len(suffix), word[:-len(suffix)]))
    for suffix in group1:
        start = len(word) - len(suffix)
        if word.endswith(suffix) and start >= rv and start >= 1 and word[start - 1] in "ая":
            best = max(best or (0, ""), (len(suffix), word[:-len(suffix)]))
    return best[1] if best else None


@lru_cache(maxsize=200_000)
def stem(word: str) -> str:
    word = word.lower().replace("ё", "е")
    if len(word) <= 3 or not re.fullmatch(r"[а-я]+", word):
        return word
    rv, r2 = _regions(word)
    # Шаг 1
    result = _strip(word, rv, PERFECTIVE_1, PERFECTIVE_2)
    if result is None:
        word = _strip(word, rv, (), REFLEXIVE) or word
        adjective = _strip(word, rv, (), ADJECTIVE)
        if adjective is not None:
            word = _strip(adjective, rv, PARTICIPLE_1, PARTICIPLE_2) or adjective
        else:
            verb = _strip(word, rv, VERB_1, VERB_2)
            word = verb if verb is not None else (_strip(word, rv, (), NOUN) or word)
    else:
        word = result
    # Шаг 2
    if word.endswith("и") and len(word) - 1 >= rv:
        word = word[:-1]
    # Шаг 3
    for suffix in DERIVATIONAL:
        if word.endswith(suffix) and len(word) - len(suffix) >= r2:
            word = word[:-len(suffix)]
            break
    # Шаг 4
    if word.endswith("нн"):
        word = word[:-1]
    else:
        for suffix in SUPERLATIVE:
            if word.endswith(suffix) and len(word) - len(suffix) >= rv:
                word = word[:-len(suffix)]
                if word.endswith("нн"):
                    word = word[:-1]
                break
        else:
            if word.endswith("ь") and len(word) - 1 >= rv:
                word = word[:-1]
    return word


STOPWORDS = set("""
а без более бы был была были было быть в вам вас весь во вот все всего всех вы где да даже для до его ее
если есть еще же за здесь и из или им их к как ко когда кто ли либо мне может мы на над надо не него нее нет
ни них но ну о об однако он она они оно от очень по под при с со так также такой там те тем то того тоже той
только том ты у уже хотя чего чей чем что чтобы чье чья эта эти это я ли бы же ведь вообще просто нам нас
наш наша наше наши свой своя свое свои который которая которое которые этот эту этого этой этом
""".split())

TOKEN_RE = re.compile(r"[a-zа-яё]+|\d+(?:[.,]\d+)*")


def normalize(text: str) -> str:
    return text.lower().replace("ё", "е")


def tokens(text: str) -> list[str]:
    """Основы слов и числа для BM25. Число «3.1» даёт и «3.1», и части не даёт: пункты различаются."""
    result = []
    for token in TOKEN_RE.findall(normalize(text)):
        if token[0].isdigit():
            result.append(token.replace(",", "."))
        elif token not in STOPWORDS and len(token) > 1:
            result.append(stem(token))
    return result


def content_stems(text: str) -> set[str]:
    return {token for token in tokens(text) if not token[0].isdigit()}
