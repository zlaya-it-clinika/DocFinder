"""Ответ на один вопрос: поиск → модель → разбор → проверка → повтор → запасной ответ.

Без модели (нет API, кончилось время, проверка не прошла дважды) ответ собирается
из найденных пунктов дословно: это не выдумка и лучше пустого ответа.
"""
from __future__ import annotations

import re
import sys
import time
from dataclasses import dataclass, field

import formatter
import prompts
import retrieval
import verify
from dialog import Dialog, parse_dialog
from index import SearchIndex
from kb import KnowledgeBase
from llm_client import ApiError, LlmClient
from retrieval import ContextItem, Retrieved
from sanitize import looks_like_override

MIN_COVERAGE = 0.25          # ниже - по теме вопроса в базе ничего нет
OUT_OF_SCOPE_COVERAGE = 0.35 # более строгая граница для явно непрофильного вопроса без юридических слов
EXTRACT_WORDS = 70           # длина дословной цитаты в запасном ответе
REFUSAL = ("В базе знаний нет документов по этому вопросу, поэтому ответить по документам я не могу. "
           "По памяти отвечать не буду: без подтверждающих положений вывод может оказаться неверным. "
           "Уточните вопрос или обратитесь к профильному юристу.")
OUT_OF_SCOPE = ("Я юридический ассистент и отвечаю на вопросы по договорам, внутренним документам компании "
                "и законам из базы знаний. Этот вопрос вне моей компетенции.")
LEGAL_HINT = re.compile(
    r"договор|соглашен|закон|кодекс|стать|пункт|регламент|норм|прав|обязан|штраф|налог|ндс|ндфл|"
    r"трудов|работ|сотрудник|увольн|испытан|отпуск|зарплат|оплат|аренд|постав|претенз|гарант|пособи|"
    r"согласован|персональн|данн|ответствен|санкц|контрагент|юрист|суд|срок|можно ли|нужно ли|"
    r"архив|оригинал|подпис|приказ|комплаенс|мрот|сверхуроч|выплат|компенсац|льгот|"
    r"ключ|обеспечительн|парт|цен|заявк|аванс|сч[её]т|компьютер|техник|поставщик|покупатель|"
    r"редакц|гендиректор|генеральн\w*\s+директор|финансов\w*\s+директор|порог|сумм|одобрен|п\.\s*\d",
    re.IGNORECASE)
WEATHER_QUESTION = re.compile(r"(?:какая|какой|каково|что\s+с)\s+(?:сейчас\s+)?погод", re.IGNORECASE)
SET_OFF_CLAIM = re.compile(r"может\s+зачитываться|подлежит\s+зач[её]ту|допускается\s+зач[её]т",
                           re.IGNORECASE)
SET_OFF_SOURCE = re.compile(r"зач[её]т|зачитыва", re.IGNORECASE)


@dataclass
class Citation:
    label: str
    doc_id: str
    document: str
    path: str
    section: str
    quote: str
    status: str = ""


@dataclass
class Result:
    kind: str
    text: str
    citations: list[Citation] = field(default_factory=list)
    missing: str = ""
    mode: str = "llm"           # llm | extractive | rule
    problems: list[str] = field(default_factory=list)
    seconds: float = 0.0


def best_items(retrieved: Retrieved, count: int) -> list[ContextItem]:
    ranked = retrieved.ranked_items()
    if not ranked:
        return []
    top = retrieved.scores.get(ranked[0].chunk.index, 0.0)
    # Второй фрагмент - только если он почти так же близок к вопросу
    return [item for position, item in enumerate(ranked[:count])
            if position == 0 or retrieved.scores.get(item.chunk.index, 0.0) >= 0.75 * top]


def where_of(item: ContextItem) -> str:
    path = formatter.short_path(item)
    if not path:
        return item.doc.display_name
    if item.doc.category == "law":
        return f"{item.doc.act}, {path}"
    return f"{item.doc.display_name}, {path}"


def to_citation(item: ContextItem) -> Citation:
    return Citation(label=item.label, doc_id=item.doc.doc_id, document=item.doc.display_name,
                    path=item.chunk.path, section=item.chunk.section, quote=item.chunk.text,
                    status="; ".join(item.marks))


def extractive(retrieved: Retrieved, relevant: bool) -> Result:
    if not relevant or not retrieved.items:
        return Result("refusal", REFUSAL, mode="extractive")
    best = best_items(retrieved, 2)
    parts = []
    for item in best:
        words = item.chunk.text.split()
        quote = " ".join(words[:EXTRACT_WORDS]) + ("…" if len(words) > EXTRACT_WORDS else "")
        parts.append(f"По документу «{where_of(item)}»: «{quote}»")
    text = formatter.to_single_line(" ".join(parts) + " Это дословная цитата найденного положения; "
                                    "проверьте, отвечает ли она на ваш вопрос.")
    return Result("partial", text, citations=[to_citation(item) for item in best], mode="extractive")


def check(parsed: verify.ModelAnswer, retrieved: Retrieved, question: str) -> list[str]:
    if not parsed.text.strip():
        return ["пустой ответ"]
    by_label = retrieved.by_label()
    problems = []
    unknown = verify.unknown_labels(parsed.text, set(by_label))
    if unknown:
        problems.append(f"ссылки на несуществующие фрагменты: {', '.join(unknown)}")
    # Число или пункт допустимы, если они есть в любом переданном модели фрагменте: модель видела их все,
    # а выдуманным считается только то, чего нет во всём контексте. Модель нередко не отмечает в USED
    # соседний фрагмент, из которого взяла оговорку.
    sources = [question] + retrieved.facts + retrieved.warnings
    sources += [f"{item.doc.display_name}\n{item.doc.title}\n{item.chunk.path}\n{item.chunk.text}"
                for item in retrieved.items]
    numbers = verify.unsupported_numbers(parsed.text, sources)
    if numbers:
        problems.append(f"числа без подтверждения во фрагментах: {', '.join(numbers)}")
    refs = verify.unsupported_refs(parsed.text, [item.chunk.path for item in retrieved.items],
                                   [item.chunk.text for item in retrieved.items])
    if refs:
        problems.append(f"ссылки на положения, которых нет в использованных фрагментах: {', '.join(refs)}")
    if (any(not re.search(r"не\s*$", parsed.text[max(0, match.start() - 5):match.start()], re.IGNORECASE)
            for match in SET_OFF_CLAIM.finditer(parsed.text))
            and not any(SET_OFF_SOURCE.search(item.chunk.text) for item in retrieved.items)):
        problems.append("утверждение о зачёте без подтверждающего условия договора")
    if parsed.kind in ("answer", "partial", "correction") and not parsed.used and retrieved.items:
        problems.append("нет ссылок на фрагменты [F..]")
    return problems

@dataclass
class Engine:
    kb: KnowledgeBase
    index: SearchIndex
    client: LlmClient | None = None
    top_n: int = 8

    def answer(self, question: str, dialog: Dialog | None = None, query_vector: list[float] | None = None,
               deadline: float | None = None) -> Result:
        started = time.monotonic()
        dialog = dialog or parse_dialog(question)
        try:
            result = self._answer(dialog, query_vector, deadline)
        except Exception as exc:  # ответ должен быть всегда: ошибка одного вопроса не роняет прогон
            print(f"[engine] ошибка: {type(exc).__name__}: {exc}", file=sys.stderr)
            result = Result("refusal", REFUSAL, mode="rule", problems=[type(exc).__name__])
        result.seconds = round(time.monotonic() - started, 2)
        return result

    def llm_ready(self, deadline: float | None, reserve: float = 5.0) -> bool:
        return (self.client is not None and self.client.available("chat")
                and (deadline is None or time.monotonic() < deadline - reserve))

    def _answer(self, dialog: Dialog, query_vector, deadline) -> Result:
        retrieved = retrieval.retrieve(self.kb, self.index, dialog, self.client, query_vector, self.top_n, deadline)
        last = dialog.last_user
        if WEATHER_QUESTION.search(last):
            return Result("out_of_scope", OUT_OF_SCOPE, mode="rule")
        if re.search(r"\bGDPR\b", last, re.IGNORECASE) and not any(
                "gdpr" in doc.text.lower() for doc in self.kb.docs):
            return Result("refusal", REFUSAL, mode="rule")
        if retrieved.entities.unknown_numbers and not retrieved.entities.doc_ids:
            numbers = ", ".join(retrieved.entities.unknown_numbers)
            return Result("refusal", f"В базе знаний нет документа с номером {numbers}. "
                           "Проверьте номер документа или уточните вопрос.", mode="rule")
        relevant = retrieved.coverage >= MIN_COVERAGE or bool(retrieved.entities.doc_ids)
        if not relevant and re.search(r"по памяти|без документов", last, re.IGNORECASE):
            return Result("refusal", REFUSAL, mode="rule")
        if (not retrieved.entities.doc_ids and retrieved.coverage < OUT_OF_SCOPE_COVERAGE
                and not LEGAL_HINT.search(" ".join(dialog.user_texts()))):
            return Result("out_of_scope", OUT_OF_SCOPE, mode="rule")
        if not self.llm_ready(deadline):
            return extractive(retrieved, relevant)
        messages = prompts.build_messages(self.kb.own_org, last, dialog.context_text(), retrieved,
                                          looks_like_override(last), low_match=not relevant)
        try:
            raw = self.client.chat(messages, deadline=deadline)
        except ApiError as exc:
            print(f"[engine] модель недоступна: {exc}", file=sys.stderr)
            return extractive(retrieved, relevant)
        parsed = verify.parse_answer(raw)
        problems = check(parsed, retrieved, last)
        if problems and self.llm_ready(deadline, reserve=10.0):
            repair = messages + [
                {"role": "assistant", "content": raw},
                {"role": "user", "content": prompts.REPAIR_PROMPT.format(
                    problems="\n".join(f"- {problem}" for problem in problems))},
            ]
            try:
                raw = self.client.chat(repair, deadline=deadline)
                parsed = verify.parse_answer(raw)
                problems = check(parsed, retrieved, last)
            except ApiError:
                pass
        if problems:
            print(f"[engine] ответ не прошёл проверку: {problems}", file=sys.stderr)
            fallback = extractive(retrieved, relevant)
            fallback.problems = problems
            return fallback
        return self._finish(parsed, retrieved, relevant)

    def _finish(self, parsed: verify.ModelAnswer, retrieved: Retrieved, relevant: bool) -> Result:
        by_label = retrieved.by_label()
        # Отказ ничего не утверждает: ссылки на найденные «по соседству» пункты в нём вводят в заблуждение
        cite_map = {} if parsed.kind in ("refusal", "out_of_scope") else by_label
        text = formatter.finalize(parsed.text, cite_map)
        if not text:
            return extractive(retrieved, relevant)
        used = [by_label[label] for label in parsed.used if label in by_label and cite_map]
        return Result(kind=parsed.kind, text=text, citations=[to_citation(item) for item in used],
                      missing=parsed.missing, mode="llm")
