"""Локальная оценка поиска, готовых ответов и A/B-вариантов.

Примеры:
    python eval_local.py --kb tests/data/demo/knowledge-base.zip --cases tests/cases.csv
    python eval_local.py --golden expected.csv --answers actual.csv
    python eval_local.py --golden expected.csv --answer-a first.csv --answer-b second.csv --judge
"""
from __future__ import annotations

import argparse
import csv
import json
import re
import sys
from dataclasses import asdict, dataclass
from pathlib import Path

import kb as kb_mod
import retrieval
import textnorm
from config import load_config
from dialog import parse_dialog
from index import SearchIndex
from llm_client import ApiError, LlmClient
from pipeline import Engine


@dataclass
class CaseResult:
    case_id: str
    ok: bool
    problems: list[str]
    coverage: float
    items: int
    chars: int


def read_csv(path: Path) -> list[dict[str, str]]:
    with path.open(encoding="utf-8-sig", newline="") as file:
        return [{key: (value or "").strip() for key, value in row.items()} for row in csv.DictReader(file)]


def read_answers(path: Path | None) -> dict[str, str]:
    if path is None:
        return {}
    return {row["id"]: row["answer"] for row in read_csv(path)}


def split_values(value: str) -> list[str]:
    return [item.strip() for item in value.split("|") if item.strip()]


def run_cases(kb_path: Path, cases_path: Path, answers_path: Path | None = None) -> list[CaseResult]:
    base = kb_mod.load(kb_path)
    index = SearchIndex(base)
    engine = Engine(base, index, None)
    answers = read_answers(answers_path)
    results: list[CaseResult] = []
    for case in read_csv(cases_path):
        dialog = parse_dialog(case["question"])
        found = retrieval.retrieve(base, index, dialog)
        problems: list[str] = []
        expected_doc = case.get("expected_doc", "")
        expected_path = case.get("expected_path", "")
        if expected_doc and not any(expected_doc.lower() in item.doc.display_name.lower() for item in found.items):
            problems.append(f"нет документа {expected_doc}")
        if expected_path and not any(
            item.chunk.path == expected_path
            and (not expected_doc or expected_doc.lower() in item.doc.display_name.lower())
            for item in found.items
        ):
            problems.append(f"нет положения {expected_doc} {expected_path}".strip())
        chars = sum(len(item.chunk.text) for item in found.items)
        if chars > retrieval.CONTEXT_CHARS:
            problems.append(f"контекст {chars} > {retrieval.CONTEXT_CHARS}")
        expected_kind = case.get("expected_kind", "")
        if expected_kind in ("refusal", "out_of_scope"):
            actual = engine.answer(case["question"], dialog=dialog).kind
            if actual != expected_kind:
                problems.append(f"вид ответа {actual}, ожидался {expected_kind}")
        answer = answers.get(case["id"], "")
        if answers and not answer:
            problems.append("нет готового ответа")
        if answer:
            low = answer.lower().replace("ё", "е")
            for term in split_values(case.get("required_terms", "")):
                if term.lower().replace("ё", "е") not in low:
                    problems.append(f"нет обязательного текста {term!r}")
            for term in split_values(case.get("forbidden_terms", "")):
                if term.lower().replace("ё", "е") in low:
                    problems.append(f"есть запрещённый текст {term!r}")
        results.append(CaseResult(case["id"], not problems, problems, found.coverage, len(found.items), chars))
    return results


def lexical_score(golden: str, answer: str) -> tuple[float, float, float]:
    expected = textnorm.content_stems(golden)
    actual = textnorm.content_stems(answer)
    overlap = len(expected & actual)
    recall = overlap / len(expected) if expected else 1.0
    precision = overlap / len(actual) if actual else 0.0
    f1 = 2 * recall * precision / (recall + precision) if recall + precision else 0.0
    return recall, precision, f1


def compare_with_golden(golden_path: Path, answers_path: Path) -> dict[str, float]:
    golden = read_answers(golden_path)
    answers = read_answers(answers_path)
    scores = [lexical_score(golden[qid], answers.get(qid, "")) for qid in golden]
    count = len(scores) or 1
    return {
        "questions": len(scores),
        "recall": sum(score[0] for score in scores) / count,
        "precision": sum(score[1] for score in scores) / count,
        "f1": sum(score[2] for score in scores) / count,
    }


JUDGE_SYSTEM = """Ты строгий LLM-судья юридического RAG. Сравни два ответа с эталоном.
Выдуманный факт, число или ссылка делает ответ проигравшим. Затем оцени полноту, прямоту ответа и точность источников.
Ответь ровно одной строкой: WINNER: A, WINNER: B или WINNER: TIE."""


def judge_once(client: LlmClient, question: str, golden: str, answer_a: str, answer_b: str) -> str:
    prompt = (f"ВОПРОС:\n{question}\n\nЭТАЛОН:\n{golden}\n\nОТВЕТ A:\n{answer_a}\n\n"
              f"ОТВЕТ B:\n{answer_b}\n\nВыбери ответ, который ближе к эталону и безопаснее.")
    raw = client.chat([{"role": "system", "content": JUDGE_SYSTEM}, {"role": "user", "content": prompt}])
    match = re.search(r"WINNER\s*:\s*(A|B|TIE)", raw, re.IGNORECASE)
    return match.group(1).upper() if match else "TIE"


def pairwise_judge(questions_path: Path, golden_path: Path, answer_a_path: Path,
                   answer_b_path: Path) -> dict[str, object]:
    questions = {row["id"]: row["question"] for row in read_csv(questions_path)}
    golden = read_answers(golden_path)
    first = read_answers(answer_a_path)
    second = read_answers(answer_b_path)
    wins = losses = ties = 0
    details = []
    cfg = load_config()
    if not cfg.llm_ready:
        raise SystemExit("Для --judge нужны LLM_BASE_URL, LLM_API_KEY и LLM_MODEL")
    with LlmClient(cfg) as client:
        for qid, expected in golden.items():
            try:
                round_one = judge_once(client, questions[qid], expected, first.get(qid, ""), second.get(qid, ""))
                round_two = judge_once(client, questions[qid], expected, second.get(qid, ""), first.get(qid, ""))
            except ApiError as exc:
                details.append({"id": qid, "result": "error", "error": str(exc)})
                continue
            votes = [("a" if round_one == "A" else "b" if round_one == "B" else "tie"),
                     ("b" if round_two == "A" else "a" if round_two == "B" else "tie")]
            if votes.count("b") > votes.count("a"):
                result = "b"
                wins += 1
            elif votes.count("a") > votes.count("b"):
                result = "a"
                losses += 1
            else:
                result = "tie"
                ties += 1
            details.append({"id": qid, "result": result, "rounds": [round_one, round_two]})
    return {"wins_b": wins, "losses_b": losses, "ties": ties, "details": details}


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--kb", type=Path)
    parser.add_argument("--cases", type=Path)
    parser.add_argument("--answers", type=Path)
    parser.add_argument("--questions", type=Path)
    parser.add_argument("--golden", type=Path)
    parser.add_argument("--answer-a", type=Path)
    parser.add_argument("--answer-b", type=Path)
    parser.add_argument("--judge", action="store_true")
    parser.add_argument("--json", type=Path)
    args = parser.parse_args()
    report: dict[str, object] = {}
    failed = False
    if args.kb and args.cases:
        cases = run_cases(args.kb, args.cases, args.answers)
        failures = [asdict(result) for result in cases if not result.ok]
        report["cases"] = {"total": len(cases), "passed": len(cases) - len(failures), "failures": failures}
        failed = bool(failures)
    if args.golden and args.answers:
        report["golden"] = compare_with_golden(args.golden, args.answers)
    if args.judge:
        required = (args.questions, args.golden, args.answer_a, args.answer_b)
        if not all(required):
            parser.error("--judge требует --questions, --golden, --answer-a и --answer-b")
        report["judge"] = pairwise_judge(args.questions, args.golden, args.answer_a, args.answer_b)
    rendered = json.dumps(report, ensure_ascii=False, indent=2)
    print(rendered)
    if args.json:
        args.json.write_text(rendered + "\n", encoding="utf-8")
    raise SystemExit(1 if failed else 0)


if __name__ == "__main__":
    try:
        sys.stdout.reconfigure(encoding="utf-8")
    except (AttributeError, ValueError):
        pass
    main()
