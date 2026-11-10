"""Разбор базы ПСБ: метаданные, пути положений, справки Гаранта, ловушки."""
import unittest
from datetime import date

from tests.common import knowledge_base


def doc_by(prefix: str):
    kb = knowledge_base()
    return next(doc for doc in kb.docs if doc.doc_id.startswith(prefix))


def paths(doc) -> list[str]:
    return [chunk.path for chunk in doc.chunks]


class ParseCompanyDocs(unittest.TestCase):
    def test_contract_metadata(self):
        doc = doc_by("договор-аренды")
        self.assertEqual(doc.doc_type, "contract")
        self.assertEqual(doc.number, "А-08/2024")
        self.assertEqual(doc.counterparty, "Северный Склад")
        self.assertEqual(doc.doc_date, date(2024, 8, 1))
        self.assertIn("№ А-08/2024", doc.display_name)

    def test_contract_clauses(self):
        doc = doc_by("договор-аренды")
        self.assertIn("п. 3.1", paths(doc))
        clause = next(c for c in doc.chunks if c.path == "п. 4.3")
        self.assertIn("круглосуточно", clause.text)
        self.assertEqual(clause.section, "4. Права и обязанности сторон")

    def test_similar_counterparties(self):
        self.assertEqual(doc_by("договор-поставки-п-17").counterparty, "Альфа-Трейд")
        self.assertEqual(doc_by("договор-поставки-пт-05").counterparty, "Альфа-Трейдинг")

    def test_supplement_linked(self):
        kb = knowledge_base()
        supplement = doc_by("доп-соглашение")
        contract = doc_by("договор-поставки-п-17")
        self.assertEqual(supplement.parent_id, contract.doc_id)
        self.assertIn(supplement.doc_id, contract.supplements)
        self.assertEqual(kb.amended_clauses[supplement.doc_id], ["3.2"])
        self.assertEqual(supplement.doc_date, date(2024, 9, 3))

    def test_regulation_editions(self):
        kb = knowledge_base()
        old, new = doc_by("регламент-согласования-договоров-ред-2023"), doc_by("регламент-согласования-договоров-ред-2025")
        self.assertEqual(old.group_key, new.group_key)
        self.assertEqual(old.effective_to, date(2025, 3, 1))
        self.assertEqual(new.effective_from, date(2025, 3, 1))
        self.assertIsNone(new.effective_to)
        self.assertIs(kb.edition_on(old.group_key, date(2024, 8, 15)), old)
        self.assertIs(kb.edition_on(old.group_key, date(2025, 3, 1)), new)
        self.assertIs(kb.edition_on(old.group_key, None), new)

    def test_letter_related_to_regulation(self):
        letter = doc_by("письмо-комплаенс")
        self.assertEqual(letter.number, "К-12")
        self.assertIn(doc_by("регламент-согласования-договоров-ред-2025").doc_id, letter.related)

    def test_injection_removed(self):
        memo = doc_by("служебная-записка")
        self.assertEqual(len(memo.removed_lines), 1)
        self.assertIn("ИИ-АССИСТЕНТА", memo.removed_lines[0])
        self.assertTrue(all("90 календарных" not in chunk.text for chunk in memo.chunks))
        self.assertTrue(any("не более 3 (трёх) рабочих дней" in chunk.text for chunk in memo.chunks))


class ParseLaws(unittest.TestCase):
    def test_tk_parts_without_numbers(self):
        doc = doc_by("тк-ст70")
        self.assertEqual(doc.act, "ТК РФ")
        self.assertEqual(doc.article, "70")
        part5 = next(c for c in doc.chunks if c.path == "ст. 70, ч. 5")
        self.assertIn("шести месяцев", part5.text)
        part3 = next(c for c in doc.chunks if c.path == "ст. 70, ч. 3")
        self.assertIn("распространяются положения трудового законодательства", part3.text)

    def test_tk59_seasonal_in_part1(self):
        doc = doc_by("тк-ст59")
        part1 = " ".join(c.text for c in doc.chunks if c.clause == "1")
        self.assertIn("сезонных работ", part1)
        self.assertIn(date(2027, 3, 1), [day for day, _ in doc.amendments])

    def test_federal_law_numbered_parts(self):
        doc = doc_by("152фз")
        self.assertEqual(doc.act, "152-ФЗ")
        part2 = next(c for c in doc.chunks if c.path == "ст. 9, ч. 2")
        self.assertIn("может быть отозвано", part2.text)

    def test_tax_code_points(self):
        doc = doc_by("нк-ст164")
        point3 = next(c for c in doc.chunks if c.path == "ст. 164, п. 3")
        self.assertIn("22 процента", point3.text)
        self.assertTrue(any("1 января 2026" in note for note in point3.notes))
        self.assertTrue(all(len(c.text) <= 1700 for c in doc.chunks))

    def test_garant_notes_not_in_text(self):
        for doc in knowledge_base().docs:
            for chunk in doc.chunks:
                self.assertNotIn("Источник:", chunk.text)
                self.assertNotIn("Дата выгрузки", chunk.text)
            if doc.category == "law":
                self.assertEqual(doc.exported_at, date(2026, 10, 10))

    def test_mrot_heading_is_norm(self):
        doc = doc_by("мрот")
        self.assertTrue(any("27 093" in chunk.text for chunk in doc.chunks))


if __name__ == "__main__":
    unittest.main()
