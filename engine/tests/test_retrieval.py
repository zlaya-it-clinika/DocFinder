"""Отбор контекста по 18 демо-вопросам: нужные положения попадают в промпт (без модели и эмбеддингов).

Ожидания взяты из эталонных ответов demo_output.csv.
"""
import unittest

import retrieval
from dialog import parse_dialog
from tests.common import demo_questions, knowledge_base, search_index

# id -> [(часть display_name документа, путь положения)]
EXPECTED = {
    "1": [("А-08/2024", "п. 3.1"), ("А-08/2024", "п. 3.2"), ("А-08/2024", "п. 3.3")],
    "2": [("ТК РФ, ст. 70", "ст. 70, ч. 5")],
    "4": [("А-08/2024", "п. 3.1"), ("А-08/2024", "п. 3.4")],
    "5": [("ред. от 10.01.2023", "п. 2.4"), ("ред. от 01.03.2025", "п. 2.4")],
    "6": [("П-17/2024", "п. 6.1"), ("П-17/2024", "п. 3.3")],
    "7": [("А-08/2024", "п. 4.3")],
    "8": [("П-17/2024", "п. 6.2")],
    "9": [("ПТ-05/2023", "п. 4.2"), ("ПТ-05/2023", "п. 4.1")],
    "10": [("ред. от 01.03.2025", "п. 1.3")],
    "11": [("ЮО-7", "")],
    "12": [("ТК РФ, ст. 70", "ст. 70, ч. 3")],
    "13": [("152-ФЗ, ст. 9", "ст. 9, ч. 2")],
    "14": [("ТК РФ, ст. 59", "ст. 59, ч. 1")],
    "15": [("А-08/2024", "п. 6.3")],
    "16": [("П-17/2024", "п. 2.1")],
    "17": [("ПТ-05/2023", "п. 6.1")],
    "18": [("ред. от 01.03.2025", "п. 3.2")],
}


def retrieve(qid: str):
    return retrieval.retrieve(knowledge_base(), search_index(), parse_dialog(demo_questions()[qid]))


class DemoRetrieval(unittest.TestCase):
    def test_expected_clauses_in_context(self):
        for qid, expected in EXPECTED.items():
            with self.subTest(qid=qid):
                result = retrieve(qid)
                got = [(item.doc.display_name, item.chunk.path) for item in result.items]
                for doc_part, path in expected:
                    self.assertTrue(any(doc_part in name and p == path for name, p in got),
                                    f"#{qid}: нет {doc_part} {path!r} в {got[:15]}")

    def test_context_size_limited(self):
        for qid in EXPECTED:
            result = retrieve(qid)
            self.assertLessEqual(sum(len(item.chunk.text) for item in result.items), retrieval.CONTEXT_CHARS)

    def test_edition_facts_for_2024(self):
        result = retrieve("5")
        joined = " ".join(result.facts)
        self.assertIn("ред. от 10.01.2023", joined)
        self.assertIn("не применяется", joined)

    def test_current_edition_only_without_date(self):
        result = retrieve("10")
        names = {item.doc.display_name for item in result.items}
        self.assertFalse(any("10.01.2023" in name for name in names), names)

    def test_no_topic_low_coverage(self):
        self.assertLess(retrieve("3").coverage, 0.25)

    def test_long_clause_pieces_stay_together(self):
        """НДФЛ: п. 1 ст. 224 НК режется на куски; ставки (первый кусок) и оговорка (второй) должны быть вместе."""
        kb = knowledge_base()
        dialog = parse_dialog("Какая ставка НДФЛ для налоговых резидентов с обычных доходов?")
        result = retrieval.retrieve(kb, search_index(), dialog)
        pieces = [item.chunk.text for item in result.items
                  if item.doc.act == "НК РФ" and item.doc.article == "224" and item.chunk.path == "ст. 224, п. 1"]
        self.assertGreaterEqual(len(pieces), 2, pieces)
        self.assertTrue(any("13 процентов" in text for text in pieces))

    def test_named_contract_does_not_fill_context_with_every_clause(self):
        """Точный номер договора фильтрует документ, но в модель идут лучшие пункты, а не весь договор."""
        kb = knowledge_base()
        dialog = parse_dialog("Сколько дней отводится на оплату товара по договору поставки П-17/2024?")
        result = retrieval.retrieve(kb, search_index(), dialog)
        contract = kb.by_number["П-17/2024"]
        contract_items = [item for item in result.items if item.doc.doc_id == contract.doc_id]
        self.assertTrue(any(item.chunk.path == "п. 4.2" for item in contract_items),
                        [item.chunk.path for item in contract_items])
        self.assertLessEqual(len(contract_items), 10, [item.chunk.path for item in contract_items])

    def test_prompt_order_preserves_search_priority(self):
        """Лучшее доказательство получает раннюю метку вместо сортировки по порядку документов базы."""
        result = retrieval.retrieve(
            knowledge_base(), search_index(),
            parse_dialog("Какова ставка НДС при реализации продовольственных товаров, например молока?"),
        )
        self.assertTrue(result.items)
        self.assertEqual(result.items[0].doc.act, "НК РФ")
        self.assertEqual(result.items[0].doc.article, "164")

    def test_long_law_context_is_bounded(self):
        result = retrieval.retrieve(
            knowledge_base(), search_index(),
            parse_dialog("Какая ставка НДФЛ для налоговых резидентов с обычных доходов?"),
        )
        chars = sum(len(item.chunk.text) for item in result.items)
        self.assertLessEqual(chars, 10000, chars)

    def test_explicit_letter_adds_only_referenced_regulation_clause(self):
        kb = knowledge_base()
        result = retrieval.retrieve(
            kb, search_index(),
            parse_dialog("Что написано в письме комплаенс-службы К-12 и на что оно влияет?"),
        )
        self.assertEqual(result.entities.doc_ids, [kb.by_number["К-12"].doc_id])
        regulation = [item for item in result.items if item.doc.doc_type == "regulation"]
        self.assertTrue(any(item.chunk.path == "п. 5.4" for item in regulation),
                        [(item.doc.display_name, item.chunk.path) for item in result.items])
        self.assertLessEqual(len(regulation), 4, [item.chunk.path for item in regulation])

    def test_law_question_does_not_pull_unrelated_contract_supplement(self):
        result = retrieval.retrieve(
            knowledge_base(), search_index(),
            parse_dialog("Какова ставка НДС при реализации продовольственных товаров, например молока?"),
        )
        self.assertFalse(any("дополнительное соглашение" in fact.lower() for fact in result.facts), result.facts)

    def test_focused_contract_does_not_include_another_contract(self):
        kb = knowledge_base()
        result = retrieval.retrieve(kb, search_index(), parse_dialog(demo_questions()["17"]))
        focused = kb.by_number["ПТ-05/2023"].doc_id
        contracts = {item.doc.doc_id for item in result.items if item.doc.doc_type == "contract"}
        self.assertEqual(contracts, {focused})

    def test_trading_not_trade(self):
        result = retrieve("17")
        self.assertEqual(result.entities.doc_ids, [knowledge_base().by_number["ПТ-05/2023"].doc_id])


if __name__ == "__main__":
    unittest.main()
