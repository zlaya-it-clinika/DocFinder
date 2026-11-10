"""Проверка против эталона: эталонные ответы demo_output.csv должны проходить сверку чисел и ссылок
на контексте, который отбирает поиск. Иначе либо поиск теряет нужные положения, либо проверка
ложно блокирует правильный ответ."""
import csv
import unittest

import retrieval
import verify
from dialog import parse_dialog
from tests.common import SOURCE_OUTPUT, demo_questions, knowledge_base, search_index


def golden() -> dict[str, str]:
    with SOURCE_OUTPUT.open(encoding="utf-8-sig", newline="") as f:
        return {row["id"]: row["answer"] for row in csv.DictReader(f)}


@unittest.skipUnless(SOURCE_OUTPUT.is_file(), "нет demo_output.csv")
class GoldenAnswersPassVerification(unittest.TestCase):
    def test_numbers_and_refs_supported(self):
        questions = demo_questions()
        for qid, answer in golden().items():
            with self.subTest(qid=qid):
                question = questions[qid]
                result = retrieval.retrieve(knowledge_base(), search_index(), parse_dialog(question))
                sources = [question] + result.facts + result.warnings + [
                    f"{item.doc.display_name}\n{item.doc.title}\n{item.chunk.path}\n{item.chunk.text}"
                    for item in result.items]
                self.assertEqual(verify.unsupported_numbers(answer, sources), [], answer)
                refs = verify.unsupported_refs(answer, [item.chunk.path for item in result.items],
                                               [item.chunk.text for item in result.items] + [question])
                self.assertEqual(refs, [], answer)


if __name__ == "__main__":
    unittest.main()
