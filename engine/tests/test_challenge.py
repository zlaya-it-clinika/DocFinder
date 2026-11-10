"""Сложные вопросы по базе, датам, похожим договорам и неверным посылкам."""
import unittest
from pathlib import Path

from eval_local import run_cases
from tests.common import ensure_demo_data


class ChallengeCases(unittest.TestCase):
    def test_retrieval_and_rules(self):
        cases = Path(__file__).with_name("challenge_cases.csv")
        results = run_cases(ensure_demo_data() / "knowledge-base.zip", cases)
        failures = {result.case_id: result.problems for result in results if not result.ok}
        self.assertEqual(failures, {})


if __name__ == "__main__":
    unittest.main()
