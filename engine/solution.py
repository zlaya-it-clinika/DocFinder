"""Точка входа сабмита ПСБ «Юридический ассистент».

Платформа запускает `python solution.py` в рабочем каталоге и передаёт в DATA_DIR:
- questions.csv (id,question) - каждый вопрос независим, диалог - в одной ячейке;
- knowledge-base.zip (TXT/MD/DOCX).
Результат - submission.csv (id,answer) в рабочем каталоге.

Порядок: база → индекс (BM25 + векторы bge-m3, если доступны) → эмбеддинги вопросов пачкой →
параллельные ответы с общим дедлайном → атомарная запись CSV. Ответ есть всегда:
при сбое модели или нехватке времени - дословная цитата найденного пункта или обоснованный отказ.
Ключи берутся только из окружения и нигде не сохраняются.
"""
from __future__ import annotations

import csv
import os
import sys
import tempfile
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path

import kb as kb_mod
from config import Config, load_config
from dialog import parse_dialog
from index import SearchIndex
from llm_client import ApiError, LlmClient, log
from pipeline import REFUSAL, Engine

KB_NAMES = ("knowledge-base.zip", "kb.zip", "knowledge_base.zip", "knowledge-base", "knowledge_base", "kb")
OUTPUT = "submission.csv"
CACHE_NAME = "kb_vectors.npz"
SAVE_EVERY = 10


def find_input(data_dir: Path, *names: str) -> Path:
    here = Path(__file__).resolve().parent
    for root in (data_dir, here / data_dir, Path.cwd(), here):
        for name in names:
            path = root / name
            if path.exists():
                return path
    raise FileNotFoundError(f"Не найден ни один из: {', '.join(names)} (DATA_DIR={data_dir})")


def load_questions(path: Path) -> list[tuple[str, str]]:
    with path.open(encoding="utf-8-sig", newline="") as f:
        reader = csv.DictReader(f)
        fields = {name.strip().lower(): name for name in reader.fieldnames or []}
        if "id" not in fields or "question" not in fields:
            raise ValueError("В questions.csv нужны колонки id и question")
        rows = [(str(row[fields["id"]]).strip(), str(row[fields["question"]] or "").strip()) for row in reader]
    seen: set[str] = set()
    unique = []
    for qid, question in rows:
        if qid and qid not in seen:
            seen.add(qid)
            unique.append((qid, question))
    if not unique:
        raise ValueError("В questions.csv нет вопросов")
    return unique


def write_submission(path: Path, questions: list[tuple[str, str]], answers: dict[str, str]) -> None:
    """Атомарно: временный файл в том же каталоге, затем замена."""
    fd, tmp = tempfile.mkstemp(prefix=".submission-", suffix=".csv", dir=str(path.parent))
    try:
        with os.fdopen(fd, "w", encoding="utf-8", newline="") as f:
            writer = csv.writer(f, quoting=csv.QUOTE_ALL, lineterminator="\n")
            writer.writerow(["id", "answer"])
            for qid, _ in questions:
                answer = (answers.get(qid) or "").strip() or REFUSAL
                writer.writerow([qid, answer[:10000]])
        os.replace(tmp, path)
    finally:
        if os.path.exists(tmp):
            os.remove(tmp)


def cache_path() -> Path:
    """Кэш из подпапки ZIP копируется во временный каталог, который доступен на запись."""
    bundled = Path(__file__).resolve().parent / "assets" / CACHE_NAME
    tmp = Path(tempfile.gettempdir()) / "psb-legal-assistant" / CACHE_NAME
    if bundled.is_file() and not tmp.exists():
        tmp.parent.mkdir(parents=True, exist_ok=True)
        tmp.write_bytes(bundled.read_bytes())
    return tmp


def embed_questions(client: LlmClient, index: SearchIndex, questions: list[tuple[str, str]],
                    deadline: float) -> dict[str, list[float]]:
    if index.vectors is None:
        return {}
    texts, ids = [], []
    for qid, question in questions:
        dialog = parse_dialog(question)
        texts.append("\n".join([dialog.last_user] + [turn.text for turn in dialog.history if turn.role == "user"]))
        ids.append(qid)
    try:
        vectors = client.embed(texts, deadline=deadline)
    except ApiError as exc:
        log(f"[solution] эмбеддинги вопросов недоступны: {exc}")
        return {}
    return dict(zip(ids, vectors))

def run(cfg: Config) -> int:
    started = time.monotonic()
    deadline = started + cfg.time_budget
    questions = load_questions(find_input(cfg.data_dir, "questions.csv"))
    output = Path.cwd() / OUTPUT
    answers: dict[str, str] = {}
    # Сразу кладём файл с отказами: даже при падении в середине на платформе будет полный CSV
    write_submission(output, questions, answers)
    base = kb_mod.load(find_input(cfg.data_dir, *KB_NAMES))
    log(f"[solution] вопросов {len(questions)}; база: {base.summary()}")
    if not cfg.llm_ready:
        log("[solution] LLM_BASE_URL/LLM_MODEL не заданы: ответы без модели (дословные цитаты)")
    modes: dict[str, int] = {}
    with LlmClient(cfg) as client:
        index = SearchIndex(base)
        index.build_vectors(client, cache_path(), deadline=started + cfg.time_budget * 0.25)
        query_vectors = embed_questions(client, index, questions, started + cfg.time_budget * 0.3)
        engine = Engine(base, index, client if cfg.llm_ready else None, top_n=cfg.reranker_top_n)

        def work(qid: str, question: str) -> tuple[str, str, str]:
            result = engine.answer(question, query_vector=query_vectors.get(qid), deadline=deadline)
            return qid, result.text, result.mode

        with ThreadPoolExecutor(max_workers=cfg.max_workers) as pool:
            futures = [pool.submit(work, qid, question) for qid, question in questions]
            for done, future in enumerate(as_completed(futures), 1):
                try:
                    qid, text, mode = future.result()
                except Exception as exc:  # Engine.answer сам ловит ошибки; это последняя страховка
                    log(f"[solution] ошибка вопроса: {type(exc).__name__}")
                    continue
                answers[qid] = text
                modes[mode] = modes.get(mode, 0) + 1
                if done % SAVE_EVERY == 0 or done == len(questions):
                    write_submission(output, questions, answers)
                    log(f"[solution] готово {done}/{len(questions)} за {time.monotonic() - started:.0f} с")
    write_submission(output, questions, answers)
    log(f"[solution] {OUTPUT}: {len(questions)} ответов, режимы {modes}, {time.monotonic() - started:.0f} с")
    return 0


def main() -> None:
    for stream in (sys.stdout, sys.stderr):
        try:
            stream.reconfigure(encoding="utf-8")
        except (AttributeError, ValueError):
            pass
    sys.exit(run(load_config()))


if __name__ == "__main__":
    main()
