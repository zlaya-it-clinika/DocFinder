"""Новые 200 вопросов: пункт документа, редакция и правила отказа."""
import unittest
from pathlib import Path

from eval_local import run_cases
from tests.common import ensure_demo_data


class Challenge200(unittest.TestCase):
    def test_retrieval_and_rules(self):
        cases = Path(__file__).with_name("challenge_200.csv")
        results = run_cases(ensure_demo_data() / "knowledge-base.zip", cases)
        self.assertEqual(len(results), 200)
        failures = {result.case_id: result.problems for result in results if not result.ok}
        self.assertEqual(failures, {})


if __name__ == "__main__":
    unittest.main()
