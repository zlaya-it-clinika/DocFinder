"""Разбор документов базы знаний: метаданные и положения (пункты, статьи, части).

Документы компании режутся по пунктам «N.N.», законы - по частям/пунктам/подпунктам статьи.
Справки Гаранта о редакциях («Пункт 3 изменен с ... - Федеральный закон ...») не являются нормой:
они хранятся как notes положения и как список изменений документа.
"""
from __future__ import annotations

import re
from collections import Counter
from dataclasses import dataclass, field
from datetime import date

import dates
from kb_loader import RawDocument
from sanitize import strip_injections

LAW = "law"
COMPANY = "company"
OTHER = "other"

MAX_CHUNK_CHARS = 1600
INTRO_CHARS = 300


@dataclass
class Chunk:
    index: int = -1          # позиция в общем списке фрагментов базы
    doc_id: str = ""
    path: str = ""           # «п. 3.1», «ст. 70, ч. 3», «ст. 164, п. 1, пп. 2.1»
    section: str = ""        # «3. Арендная плата»
    text: str = ""
    notes: list[str] = field(default_factory=list)
    order: int = 0
    clause: str = ""         # номер пункта/части для сверки ссылок: «3.1», «3»
    subclause: str = ""      # номер подпункта: «2.1»
    kind: str = "clause"     # header | clause | section | whole


@dataclass
class Document:
    doc_id: str
    file_name: str
    category: str
    category_label: str
    doc_type: str = "other"
    title: str = ""
    display_name: str = ""
    number: str = ""
    doc_date: date | None = None
    counterparty: str = ""
    organizations: list[tuple[str, str]] = field(default_factory=list)
    parent_number: str = ""
    edition_date: date | None = None
    effective_from: date | None = None
    effective_to: date | None = None
    status_note: str = ""
    group_key: str = ""
    act: str = ""
    act_title: str = ""
    article: str = ""
    article_title: str = ""
    exported_at: date | None = None
    notes: list[str] = field(default_factory=list)
    amendments: list[tuple[date, str]] = field(default_factory=list)
    removed_lines: list[str] = field(default_factory=list)
    text: str = ""
    chunks: list[Chunk] = field(default_factory=list)
    # Заполняется при связывании документов (kb_links)
    supplements: list[str] = field(default_factory=list)
    parent_id: str = ""
    related: list[str] = field(default_factory=list)
    editions: list[str] = field(default_factory=list)


def slug(text: str) -> str:
    return re.sub(r"[^0-9a-zа-яё]+", "-", text.lower()).strip("-") or "doc"


def category_of(label: str, text: str) -> str:
    low = label.lower()
    if "закон" in low or "кодекс" in low or "law" in low:
        return LAW
    if "компан" in low or "внутрен" in low or "company" in low or "договор" in low:
        return COMPANY
    # Категории нет: закон узнаём по шапке статьи
    if re.search(r"^Статья\s+\d", text, re.MULTILINE) and re.search(r"(кодекс|Федеральн\w+ закон)", text, re.I):
        return LAW
    return COMPANY if re.search(r"^\d+\.\d+\.\s", text, re.MULTILINE) else OTHER


# --- справки о редакции ---

NOTE_START = re.compile(
    r"^(Федеральн\w+\s+закон\w*\s+от|О\s+конституционно|О\s+проверке\s+конституционности|См\.\s|Действие\s+положени)",
    re.IGNORECASE)
NOTE_APPLY = re.compile(r"^Положения\s.{0,250}\(в\s+редакции", re.IGNORECASE)
NOTE_UNIT = re.compile(r"^(Статья|Статьи|Часть|Части|Пункт|Пункты|Подпункт|Подпункты|Абзац|Абзацы|Глава)\b")
NOTE_VERB = re.compile(r"(изменен|дополнен|изложен|утратил|признан|введен)", re.IGNORECASE)
AMEND_DATE = re.compile(r"\bс\s+(\d{1,2}\s+[а-яё]+\s+\d{4}|\d{2}\.\d{2}\.\d{4})", re.IGNORECASE)


def is_note(line: str) -> bool:
    text = line.strip()
    if not text:
        return False
    if NOTE_START.match(text) or NOTE_APPLY.match(text):
        return True
    # «Пункт 3 изменен с 1 января 2026 г. - Федеральный закон ...», но не «Часть вторая утратила силу.»
    return bool(NOTE_UNIT.match(text) and NOTE_VERB.search(text) and re.search(r"Федеральн", text))


def amendment_of(note: str) -> tuple[date, str] | None:
    if not NOTE_VERB.search(note) and "вступа" not in note.lower():
        return None
    match = AMEND_DATE.search(note)
    parsed = dates.first_date(match.group(1)) if match else None
    return (parsed, note) if parsed else None


def _split_paragraph(paragraph: str, limit: int) -> list[str]:
    """Сверхдлинный абзац - по предложениям («; » и «. »), чтобы кусок не превышал лимит."""
    if len(paragraph) <= limit:
        return [paragraph]
    sentences = re.split(r"(?<=[.;])\s+", paragraph)
    pieces, current = [], ""
    for sentence in sentences:
        while len(sentence) > limit:
            cut = sentence.rfind(" ", 0, limit)
            cut = cut if cut > limit // 2 else limit
            if current:
                pieces.append(current)
                current = ""
            pieces.append(sentence[:cut])
            sentence = sentence[cut:].strip()
        if current and len(current) + len(sentence) + 1 > limit:
            pieces.append(current)
            current = sentence
        else:
            current = (current + " " + sentence).strip()
    if current:
        pieces.append(current)
    return pieces


def split_long(chunk: Chunk) -> list[Chunk]:
    """Длинное положение - на куски по абзацам; у следующих кусков в начале вводная строка."""
    if len(chunk.text) <= MAX_CHUNK_CHARS:
        return [chunk]
    lines = chunk.text.split("\n")
    first = lines[0]
    # Вводная строка («1. Налогообложение ... : …») уже может быть в начале - не дублируем
    intro = first[:INTRO_CHARS].rstrip(" …") if len(first) <= INTRO_CHARS or first.endswith("…") else ""
    budget = MAX_CHUNK_CHARS - (len(intro) + 3 if intro else 0)
    paragraphs = [piece for line in lines for piece in _split_paragraph(line, budget)]
    pieces, current = [], ""
    for paragraph in paragraphs:
        if current and len(current) + len(paragraph) + 1 > MAX_CHUNK_CHARS:
            pieces.append(current)
            current = (intro + " …") if intro and not paragraph.startswith(intro) else ""
        current = (current + "\n" + paragraph) if current else paragraph
    if current:
        pieces.append(current)
    return [Chunk(path=chunk.path, section=chunk.section, text=text.strip(),
                  notes=chunk.notes if number == 0 else [], clause=chunk.clause,
                  subclause=chunk.subclause, kind=chunk.kind)
            for number, text in enumerate(pieces)]


def paragraphs_chunks(lines: list[str], path: str = "", kind: str = "whole") -> list[Chunk]:
    text = "\n".join(line.strip() for line in lines if line.strip())
    if not text:
        return []
    return split_long(Chunk(path=path, text=text, kind=kind))


# --- законы ---

SOURCE_RE = re.compile(r"^Источник:\s*(.+)$", re.MULTILINE)
EXPORT_RE = re.compile(r"^Дата выгрузки:\s*(.+)$", re.MULTILINE)
ARTICLE_HEAD = re.compile(r"^Статья\s+(\d+(?:\.\d+)*)\.?\s*(.*)$")
POINT_RE = re.compile(r"^(\d+(?:\.\d+)*(?:-\d+)?)\.\s+(.*)$")
ITEM_RE = re.compile(r"^(\d+(?:\.\d+)*(?:-\d+)?)\)\s*(.*)$")
ACT_PATTERNS = (
    (re.compile(r"Налогов\w+ кодекс|НК РФ"), "НК РФ", "Налоговый кодекс Российской Федерации"),
    (re.compile(r"Трудов\w+ кодекс|ТК РФ"), "ТК РФ", "Трудовой кодекс Российской Федерации"),
    (re.compile(r"Кодекс\w* Российской Федерации об административных|КоАП РФ"), "КоАП РФ",
     "Кодекс Российской Федерации об административных правонарушениях"),
    (re.compile(r"Гражданск\w+ кодекс|ГК РФ"), "ГК РФ", "Гражданский кодекс Российской Федерации"),
)
FZ_RE = re.compile(r"Федеральн\w+ закон\w*\s+от\s+([\d.]+)\s+N\s*(\d+-ФЗ)\s*\"?([^\"(]*)\"?")


def is_garant_heading(line: str) -> bool:
    # Подзаголовки Гаранта без знака в конце: «Cрок давности привлечения к ответственности по ст. 13.11»
    return len(line) < 120 and not re.search(r"[.;:,!?)»\"—-]$", line) and not ITEM_RE.match(line)


def parse_law(doc: Document, text: str) -> None:
    source = SOURCE_RE.search(text)
    source_line = source.group(1) if source else ""
    exported = EXPORT_RE.search(text)
    doc.exported_at = dates.first_date(exported.group(1)) if exported else None
    for pattern, short, title in ACT_PATTERNS:
        if pattern.search(source_line):
            doc.act, doc.act_title = short, title
            break
    else:
        fz = FZ_RE.search(source_line)
        if fz:
            doc.act = fz.group(2)
            doc.act_title = f"Федеральный закон от {fz.group(1)} № {fz.group(2)}" + (
                f" «{fz.group(3).strip()}»" if fz.group(3).strip() else "")
        else:
            doc.act = doc.act_title = re.sub(r"_.*$", "", doc.file_name)

    lines = [line.strip() for line in text.split("\n")]
    lines = [line for line in lines if line and not line.startswith(("Источник:", "URL:", "Дата выгрузки:"))]
    head_index = next((i for i, line in enumerate(lines) if ARTICLE_HEAD.match(line) and not is_note(line)), None)
    body_start = 0
    heading_norm = ""
    if head_index is not None:
        match = ARTICLE_HEAD.match(lines[head_index])
        doc.article = match.group(1)
        remainder = match.group(2).strip()
        # «Статья 1. Установить минимальный размер ...» - заголовок и есть норма
        if remainder.endswith(".") and remainder.rstrip(".") not in source_line:
            heading_norm = remainder
        else:
            doc.article_title = remainder
        for line in lines[:head_index]:
            _add_note(doc, line)
        body_start = head_index + 1
    else:
        art = re.search(r"ст(?:атья)?\.?\s*(\d+(?:\.\d+)*)", doc.file_name)
        doc.article = art.group(1) if art else ""

    body = lines[body_start:]
    if heading_norm:
        body.insert(0, heading_norm)
    numbered = any(POINT_RE.match(line) and not is_note(line) for line in body)
    part_label, item_label = ("п.", "пп.") if doc.act == "НК РФ" else ("ч.", "п.")
    units = _law_units_numbered(doc, body) if numbered else _law_units_plain(doc, body)
    article = f"ст. {doc.article}" if doc.article else ""
    doc.title = f"{doc.act}, {article}" + (f". {doc.article_title}" if doc.article_title else "")
    doc.display_name = f"{doc.act}, {article}".strip(", ")
    doc.doc_type = "law"
    doc.chunks = []
    for unit in units:
        doc.chunks.extend(_law_unit_chunks(unit, article, part_label, item_label))
    if not doc.chunks:
        doc.chunks = paragraphs_chunks(body, article)


def _add_note(doc: Document, line: str) -> None:
    doc.notes.append(line)
    amendment = amendment_of(line)
    if amendment:
        doc.amendments.append(amendment)

@dataclass
class _LawUnit:
    part: str
    intro: list[str] = field(default_factory=list)
    items: list[tuple[str, list[str], list[str]]] = field(default_factory=list)  # (номер, строки, notes)
    notes: list[str] = field(default_factory=list)


def _law_units_numbered(doc: Document, body: list[str]) -> list[_LawUnit]:
    """Статья с номерами «1.», «1.1.» (НК, КоАП, 152-ФЗ); «1)» - подпункты внутри."""
    units: list[_LawUnit] = []
    pending: list[str] = []
    for line in body:
        if is_note(line) or is_garant_heading(line):
            _add_note(doc, line)
            pending.append(line)
            continue
        point = POINT_RE.match(line)
        item = ITEM_RE.match(line)
        if point:
            units.append(_LawUnit(part=point.group(1), intro=[line], notes=pending))
        elif item and units:
            units[-1].items.append((item.group(1), [line], pending))
        else:
            if not units:
                units.append(_LawUnit(part="", notes=[]))
            target = units[-1]
            if target.items:
                target.items[-1][1].append(line)
            else:
                target.intro.append(line)
            target.notes.extend(pending)
        pending = []
    return units


def _law_units_plain(doc: Document, body: list[str]) -> list[_LawUnit]:
    """Статья без номеров частей (ТК): часть - абзац; список после «:» до точки - внутри части."""
    units: list[_LawUnit] = []
    pending: list[str] = []
    list_open = False
    for line in body:
        if is_note(line) or is_garant_heading(line):
            _add_note(doc, line)
            pending.append(line)
            continue
        item = ITEM_RE.match(line)
        if units and (list_open or item):
            unit = units[-1]
            if item:
                unit.items.append((item.group(1), [line], pending))
            else:
                if unit.items:
                    unit.items[-1][1].append(line)
                else:
                    unit.intro.append(line)
                unit.notes.extend(pending)
        else:
            units.append(_LawUnit(part=str(len(units) + 1), intro=[line], notes=pending))
        pending = []
        list_open = line.endswith((":", ";")) or (list_open and line.endswith(","))
    return units


def _law_unit_chunks(unit: _LawUnit, article: str, part_label: str, item_label: str) -> list[Chunk]:
    base = ", ".join(x for x in (article, f"{part_label} {unit.part}" if unit.part else "") if x)
    all_lines = unit.intro + [line for _, lines, _ in unit.items for line in lines]
    text = "\n".join(all_lines)
    if len(text) <= MAX_CHUNK_CHARS or not unit.items:
        return split_long(Chunk(path=base, text=text, notes=list(unit.notes), clause=unit.part))
    chunks = []
    if unit.intro:
        chunks.append(Chunk(path=base, text="\n".join(unit.intro), notes=list(unit.notes), clause=unit.part))
    intro_head = unit.intro[0][:INTRO_CHARS] if unit.intro else ""
    for number, lines, notes in unit.items:
        body = "\n".join(lines)
        if intro_head:
            body = intro_head + " …\n" + body
        chunks.extend(split_long(Chunk(path=f"{base}, {item_label} {number}", text=body, notes=list(notes),
                                       clause=unit.part, subclause=number)))
    return chunks

# --- документы компании ---

SECTION_RE = re.compile(r"^(\d{1,2})\.\s+([А-ЯЁA-Z][^.]{1,90})$")
CLAUSE_RE = re.compile(r"^(\d{1,2}(?:\.\d{1,2}){1,3})\.?\s+(.*)$")
ORG_RE = re.compile(r"\b(ООО|АО|ПАО|ЗАО|НАО|ИП)\s*«([^»]{2,60})»(?:\s*\(ИНН\s*(\d{10,12})\))?")
NUMBER_RE = re.compile(r"№\s*([A-ZА-ЯЁ]{1,4}-\d{1,4}(?:/\d{4}|-П)?|\d{1,4}(?:-[A-ZА-ЯЁ]{1,3})?)")
DOC_TYPES = (
    ("supplement", re.compile(r"^ДОПОЛНИТЕЛЬНОЕ\s+СОГЛАШЕНИЕ", re.I)),
    ("contract", re.compile(r"^ДОГОВОР\b", re.I)),
    ("regulation", re.compile(r"^(РЕГЛАМЕНТ|ПОЛОЖЕНИЕ|ИНСТРУКЦИЯ|ПОЛИТИКА|ПОРЯДОК)\b", re.I)),
    ("memo", re.compile(r"^СЛУЖЕБНАЯ\s+ЗАПИСКА", re.I)),
    ("order", re.compile(r"^ПРИКАЗ\b", re.I)),
)
TYPE_NAMES = {"contract": "договор", "supplement": "дополнительное соглашение", "regulation": "регламент",
              "memo": "служебная записка", "letter": "письмо", "order": "приказ", "other": "документ"}


def normalize_number(number: str) -> str:
    """«А-08/2024», «А-08-2024», «а 08/2024» -> «А-08/2024». Латиница в номере -> кириллица."""
    value = number.strip().upper().replace(" ", "-")
    value = value.translate(str.maketrans("ABCEHKMOPTXY", "АВСЕНКМОРТХУ"))
    return re.sub(r"^([А-ЯЁ]{1,4})-?(\d{1,4})[/-](\d{4})$", r"\1-\2/\3", value)


def parse_company(doc: Document, text: str) -> None:
    lines = [line.strip() for line in text.split("\n") if line.strip()]
    title_index = next((i for i, line in enumerate(lines[:6])
                        if any(pattern.match(line) for _, pattern in DOC_TYPES)), 0)
    head = lines[title_index] if lines else doc.file_name
    doc.doc_type = next((kind for kind, pattern in DOC_TYPES if pattern.match(head)), "")
    if not doc.doc_type:
        doc.doc_type = "letter" if re.search(r"Исх\.\s*№", text[:600]) else "other"
        head = next((line for line in lines[:8] if line.startswith(("О ", "Об "))), head)
    preamble = "\n".join(lines[:12])
    number = NUMBER_RE.search(head) or NUMBER_RE.search(preamble if doc.doc_type in ("letter", "memo") else "")
    doc.number = normalize_number(number.group(1)) if number else ""
    doc.title = _title_case(head)
    doc.organizations = [(match.group(2), match.group(3) or "") for match in ORG_RE.finditer(preamble)]
    doc.doc_date = _document_date(lines[:8])
    if doc.doc_type == "supplement":
        parent = re.search(r"к\s+договору[^№]*№\s*([^\s,]+)", preamble, re.I)
        doc.parent_number = normalize_number(parent.group(1)) if parent else ""
    edition = re.search(r"Редакция\s+от\s+(\d{2}\.\d{2}\.\d{4})", preamble)
    doc.edition_date = dates.first_date(edition.group(1)) if edition else None
    doc.effective_from = dates.date_after(preamble, "Действует с") or doc.edition_date
    lost = re.search(r"(утратил\w*\s+силу\s+с\s+[\d.]+)", preamble, re.I)
    if lost:
        doc.effective_to = dates.first_date(lost.group(1))
        doc.status_note = lines[0] if "утратил" in lines[0].lower() else lost.group(1)
    elif re.search(r"утратил\w*\s+силу", doc.file_name, re.I):
        doc.status_note = "утратил силу"
    if doc.doc_type == "regulation":
        doc.group_key = slug(re.sub(r"\s+(ООО|АО|ПАО)\s*«[^»]+»", "", head))
    doc.chunks = _company_chunks(lines)


def _title_case(line: str) -> str:
    if not line.isupper():
        return line
    lowered = line[0] + line[1:].lower()
    lowered = re.sub(r"«([^»]+)»", lambda m: "«" + m.group(1).title() + "»", lowered)
    lowered = re.sub(r"\b(ооо|ао|пао)\b", lambda m: m.group(1).upper(), lowered)
    # Номера документов возвращаем как в оригинале: «№ А-08/2024»
    original_numbers = iter(re.findall(r"№\s*\S+", line))
    return re.sub(r"№\s*\S+", lambda m: next(original_numbers, m.group(0)), lowered)


def _document_date(lines: list[str]) -> date | None:
    """Дата в строке с дефисом важнее «Исх. № К-12 от 15.04.2025»; строки об утрате силы не смотрим."""
    candidates = [line for line in lines if "утрат" not in line.lower()]
    for pattern in (r"(—|–)\s*\d{1,2}\s+[а-я]+\s+\d{4}", r"(Исх\.|№).{0,30}\sот\s+\d"):
        for line in candidates:
            if re.search(pattern, line):
                found = dates.first_date(line)
                if found:
                    return found
    return None

def _company_chunks(lines: list[str]) -> list[Chunk]:
    chunks: list[Chunk] = []
    preamble: list[str] = []
    section = ""
    section_lines: list[str] = []
    current: Chunk | None = None
    started = False

    def flush() -> None:
        nonlocal current, section_lines
        if current is not None:
            chunks.extend(split_long(current))
            current = None
        if section_lines:
            number = section.split(".")[0]
            chunks.extend(split_long(Chunk(path=f"разд. {number}" if number else "", section=section,
                                           text="\n".join(section_lines), clause=number, kind="section")))
            section_lines = []

    for line in lines:
        section_match = SECTION_RE.match(line)
        clause_match = CLAUSE_RE.match(line)
        if section_match and not clause_match:
            flush()
            section = f"{section_match.group(1)}. {section_match.group(2).strip()}"
            started = True
        elif clause_match:
            flush()
            number = clause_match.group(1)
            current = Chunk(path=f"п. {number}", section=section, text=line, clause=number)
            started = True
        elif not started:
            preamble.append(line)
        elif current is not None:
            current.text += "\n" + line
        else:
            section_lines.append(line)
    flush()
    if not chunks:
        # Документ без пунктов (письмо, записка, ДС): целиком или кусками по абзацам
        return paragraphs_chunks(lines, "", "whole")
    if preamble:
        chunks.insert(0, Chunk(path="преамбула", text="\n".join(preamble), kind="header"))
    return chunks


# --- сборка ---

def parse_document(raw: RawDocument, doc_id: str) -> Document:
    text, removed = strip_injections(raw.text)
    category = category_of(raw.category, text)
    doc = Document(doc_id=doc_id, file_name=raw.file_name, category=category,
                   category_label=raw.category or ("законы" if category == LAW else "документы компании"),
                   text=text, removed_lines=removed)
    if category == LAW:
        parse_law(doc, text)
    else:
        parse_company(doc, text)
    return doc


def _company_display(doc: Document, own: str) -> None:
    others = [name for name, _ in doc.organizations if name.lower() != own.lower()]
    doc.counterparty = others[0] if others else ""
    when = f" от {dates.fmt(doc.doc_date)}" if doc.doc_date else ""
    number = f" № {doc.number}" if doc.number else ""
    if doc.doc_type == "contract":
        words = re.split(r"\s+№", doc.title)[0].lower().split()
        kind = " ".join(words[:2]) if len(words) >= 2 else "договор"
        with_whom = f" с ООО «{doc.counterparty}»" if doc.counterparty else ""
        doc.display_name = f"{kind}{number}{with_whom}"
    elif doc.doc_type == "supplement":
        doc.display_name = f"дополнительное соглашение{number} к договору № {doc.parent_number}".strip()
    elif doc.doc_type == "regulation":
        name = re.sub(r"\s+(ООО|АО|ПАО)\s*«[^»]+»", "", doc.title)
        name = name[0].upper() + name[1:].lower() if name.isupper() else name
        edition = f" (ред. от {dates.fmt(doc.edition_date)})" if doc.edition_date else ""
        doc.display_name = f"{name}{edition}"
    elif doc.doc_type in ("memo", "letter", "order"):
        doc.display_name = f"{TYPE_NAMES[doc.doc_type]}{number}{when}"
    else:
        doc.display_name = doc.title or doc.file_name


def parse_all(raws: list[RawDocument]) -> list[Document]:
    used: Counter[str] = Counter()
    docs = []
    for raw in raws:
        base = slug(raw.stem)
        used[base] += 1
        doc_id = base if used[base] == 1 else f"{base}-{used[base]}"
        docs.append(parse_document(raw, doc_id))
    # Своя организация - та, что встречается в шапках документов компании чаще всех
    counts = Counter(name for doc in docs if doc.category != LAW for name, _ in doc.organizations)
    own = counts.most_common(1)[0][0] if counts else ""
    for doc in docs:
        if doc.category != LAW:
            _company_display(doc, own)
        for order, chunk in enumerate(doc.chunks):
            chunk.doc_id = doc.doc_id
            chunk.order = order
    return docs


def own_organization(docs: list[Document]) -> str:
    counts = Counter(name for doc in docs if doc.category != LAW for name, _ in doc.organizations)
    return counts.most_common(1)[0][0] if counts else ""
