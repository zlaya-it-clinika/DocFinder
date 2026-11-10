"""Расширенная retrieval-регрессия из tests/cases.csv."""
import unittest

from pathlib import Path

from eval_local import run_cases
from tests.common import ensure_demo_data


class ExtendedCases(unittest.TestCase):
    def test_all_expected_documents_and_clauses_are_retrieved(self):
        results = run_cases(ensure_demo_data() / "knowledge-base.zip", Path(__file__).with_name("cases.csv"))
        failures = {result.case_id: result.problems for result in results if not result.ok}
        self.assertEqual(failures, {})


if __name__ == "__main__":
    unittest.main()
