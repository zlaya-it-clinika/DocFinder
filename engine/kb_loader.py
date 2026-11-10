"""Чтение базы знаний: ZIP-архив или каталог с TXT, MD, DOCX.

Категория документа берётся из префикса имени файла «категория__имя» (так устроена база ПСБ)
или из первой папки внутри архива. Нечитаемые файлы пропускаются с записью в stderr.
"""
from __future__ import annotations

import io
import sys
import zipfile
from dataclasses import dataclass
from pathlib import Path, PurePosixPath
from xml.etree import ElementTree as ET

SUPPORTED = {".txt", ".md", ".docx"}
WORD_NS = "{http://schemas.openxmlformats.org/wordprocessingml/2006/main}"


@dataclass(frozen=True)
class RawDocument:
    file_name: str  # имя файла без папок, как его видит пользователь
    category: str   # «законы», «документы компании» или пусто
    stem: str       # имя без категории и расширения
    text: str


def read_docx(raw: bytes) -> str:
    """Текст DOCX по абзацам, таблицы - строками с разделителем « | »."""
    with zipfile.ZipFile(io.BytesIO(raw)) as doc:
        xml = doc.read("word/document.xml")
    root = ET.fromstring(xml)
    body = root.find(WORD_NS + "body")
    lines: list[str] = []
    for element in (body if body is not None else root):
        if element.tag == WORD_NS + "p":
            lines.append(_paragraph_text(element))
        elif element.tag == WORD_NS + "tbl":
            for row in element.iter(WORD_NS + "tr"):
                cells = [" ".join(_paragraph_text(p) for p in cell.iter(WORD_NS + "p")).strip()
                         for cell in row.iter(WORD_NS + "tc")]
                lines.append(" | ".join(cell for cell in cells if cell))
    return "\n".join(lines)


def _paragraph_text(paragraph: ET.Element) -> str:
    parts = []
    for node in paragraph.iter():
        if node.tag == WORD_NS + "t" and node.text:
            parts.append(node.text)
        elif node.tag == WORD_NS + "tab":
            parts.append("\t")
        elif node.tag in (WORD_NS + "br", WORD_NS + "cr"):
            parts.append("\n")
    return "".join(parts)


def decode_text(raw: bytes) -> str:
    for encoding in ("utf-8-sig", "cp1251"):
        try:
            return raw.decode(encoding)
        except UnicodeDecodeError:
            continue
    return raw.decode("utf-8", errors="replace")


def split_name(path: str) -> tuple[str, str, str]:
    """(имя файла, категория, основа) из пути внутри архива или каталога."""
    pure = PurePosixPath(path.replace("\\", "/"))
    file_name = pure.name
    base = pure.stem
    category = ""
    if "__" in base:
        category, base = base.split("__", 1)
    elif len(pure.parts) > 1:
        # «законы/ТК_ст70.txt»: категория - ближайшая папка
        category = pure.parts[-2]
    return file_name, category.strip(), base.strip()


def _to_document(path: str, raw: bytes) -> RawDocument | None:
    suffix = PurePosixPath(path).suffix.lower()
    if suffix not in SUPPORTED:
        return None
    try:
        text = read_docx(raw) if suffix == ".docx" else decode_text(raw)
    except (KeyError, ValueError, zipfile.BadZipFile, ET.ParseError) as exc:
        print(f"[kb] пропущен нечитаемый документ {path}: {type(exc).__name__}", file=sys.stderr)
        return None
    text = text.replace("\r\n", "\n").replace("\r", "\n").replace("\u00a0", " ").strip()
    if not text:
        return None
    file_name, category, stem = split_name(path)
    return RawDocument(file_name=file_name, category=category, stem=stem, text=text)


def _skip(name: str) -> bool:
    parts = PurePosixPath(name.replace("\\", "/")).parts
    return name.endswith("/") or any(part.startswith("__MACOSX") or part.startswith(".") for part in parts)


def load_zip(path: Path) -> list[RawDocument]:
    documents = []
    with zipfile.ZipFile(path) as archive:
        for info in sorted(archive.infolist(), key=lambda item: item.filename):
            name = _fix_zip_name(info)
            if info.is_dir() or _skip(name):
                continue
            document = _to_document(name, archive.read(info))
            if document:
                documents.append(document)
    return documents


def _fix_zip_name(info: zipfile.ZipInfo) -> str:
    # Архивы из Windows без флага UTF-8 хранят имена в cp866: возвращаем кириллицу
    if info.flag_bits & 0x800:
        return info.filename
    try:
        return info.filename.encode("cp437").decode("cp866")
    except (UnicodeEncodeError, UnicodeDecodeError):
        return info.filename


def load_directory(path: Path) -> list[RawDocument]:
    documents = []
    for file in sorted(path.rglob("*")):
        relative = file.relative_to(path).as_posix()
        if not file.is_file() or _skip(relative):
            continue
        document = _to_document(relative, file.read_bytes())
        if document:
            documents.append(document)
    return documents


def load_knowledge_base(path: Path) -> list[RawDocument]:
    documents = load_directory(path) if path.is_dir() else load_zip(path)
    if not documents:
        raise ValueError(f"В базе знаний {path} нет читаемых документов")
    return documents
