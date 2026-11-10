"""Сборка ZIP-сабмита: solution.py, модули и requirements.txt в корне, кэш в assets/.

    python build_submission.py [--out dist/submission.zip] [--with-cache путь/kb_vectors.npz]

В архив попадают только корневые .py, нужные solution.py (по импортам), без тестов, отладки и сервера.
Перед упаковкой проверяется, что зависимости есть в разрешённом списке платформы и что в файлах нет ключей.
"""
from __future__ import annotations

import argparse
import ast
import re
import sys
import zipfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
ALLOWED_REQUIREMENTS = HERE.parent.parent / "файлы" / "requirements.txt"
SECRET_RE = re.compile(r"(sk-[A-Za-z0-9]{16,}|api[_-]?key\s*=\s*['\"][^'\"]{8,}['\"])", re.IGNORECASE)


def local_imports(path: Path) -> set[str]:
    tree = ast.parse(path.read_text(encoding="utf-8"))
    names: set[str] = set()
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            names.update(alias.name.split(".")[0] for alias in node.names)
        elif isinstance(node, ast.ImportFrom) and node.module and node.level == 0:
            names.add(node.module.split(".")[0])
    return {name for name in names if (HERE / f"{name}.py").is_file()}


def collect_modules(entry: str = "solution") -> list[Path]:
    seen: set[str] = set()
    queue = [entry]
    while queue:
        name = queue.pop()
        if name in seen:
            continue
        seen.add(name)
        queue.extend(local_imports(HERE / f"{name}.py") - seen)
    return sorted(HERE / f"{name}.py" for name in seen)


def check_requirements(requirements: Path) -> None:
    pinned = [line.strip() for line in requirements.read_text(encoding="utf-8").splitlines() if line.strip()]
    bad = [line for line in pinned if "==" not in line]
    if bad:
        raise SystemExit(f"Версии не зафиксированы: {bad}")
    if ALLOWED_REQUIREMENTS.is_file():
        allowed = {line.strip().lower() for line in ALLOWED_REQUIREMENTS.read_text(encoding="utf-8").splitlines()}
        missing = [line for line in pinned if line.lower() not in allowed]
        if missing:
            raise SystemExit(f"Зависимости вне разрешённого списка платформы: {missing}")


def check_secrets(files: list[Path]) -> None:
    for file in files:
        if SECRET_RE.search(file.read_text(encoding="utf-8")):
            raise SystemExit(f"Похоже на ключ в {file.name}: проверьте файл")


def build(out: Path, cache: Path | None) -> Path:
    modules = collect_modules()
    requirements = HERE / "requirements.txt"
    check_requirements(requirements)
    check_secrets(modules)
    out.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as archive:
        for module in modules:
            archive.write(module, module.name)
        archive.write(requirements, "requirements.txt")
        if cache:
            archive.write(cache, "assets/kb_vectors.npz")
    size = out.stat().st_size
    if size > 128 * 1024 * 1024:
        raise SystemExit(f"Архив {size} байт больше лимита 128 МБ")
    print(f"{out}: {len(modules)} модулей ({', '.join(m.name for m in modules)}), {size // 1024} КБ")
    return out


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("--out", type=Path, default=HERE / "dist" / "submission.zip")
    parser.add_argument("--with-cache", type=Path, default=None, help="кэш векторов kb_vectors.npz")
    args = parser.parse_args()
    sys.stdout.reconfigure(encoding="utf-8")
    build(args.out, args.with_cache)


if __name__ == "__main__":
    main()
