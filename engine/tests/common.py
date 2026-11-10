"""Общие данные тестов из базы знаний и локальных CSV-фикстур DocFinder."""
from __future__ import annotations

import functools
import zipfile
from pathlib import Path

from tests import ENGINE_DIR

PROJECT_ROOT = ENGINE_DIR.parent
FIXTURES_DIR = ENGINE_DIR / "tests" / "fixtures"
SOURCE_KB = PROJECT_ROOT / "knowledge-base"
SOURCE_QUESTIONS = FIXTURES_DIR / "demo_questions.csv"
SOURCE_OUTPUT = FIXTURES_DIR / "demo_output.csv"
DEMO_DIR = ENGINE_DIR / "tests" / "data" / "demo"


def ensure_demo_data() -> Path:
    """Создаёт временный ZIP базы знаний для тестов загрузчика."""
    DEMO_DIR.mkdir(parents=True, exist_ok=True)
    questions = DEMO_DIR / "questions.csv"
    archive = DEMO_DIR / "knowledge-base.zip"
    if SOURCE_QUESTIONS.is_file() and not questions.is_file():
        questions.write_bytes(SOURCE_QUESTIONS.read_bytes())
    if SOURCE_KB.is_dir() and not archive.is_file():
        with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as zf:
            for path in sorted(SOURCE_KB.iterdir()):
                zf.write(path, path.name)
    return DEMO_DIR


@functools.lru_cache(maxsize=1)
def knowledge_base():
    import kb
    source = SOURCE_KB if SOURCE_KB.is_dir() else ensure_demo_data() / "knowledge-base.zip"
    return kb.load(source)


@functools.lru_cache(maxsize=1)
def search_index():
    from index import SearchIndex
    return SearchIndex(knowledge_base())


def demo_questions() -> dict[str, str]:
    import csv
    path = SOURCE_QUESTIONS if SOURCE_QUESTIONS.is_file() else ensure_demo_data() / "questions.csv"
    with path.open(encoding="utf-8-sig", newline="") as f:
        return {row["id"]: row["question"] for row in csv.DictReader(f)}
