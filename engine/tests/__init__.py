import sys
from pathlib import Path

# Модули движка лежат плоско в engine/: тесты импортируют их напрямую
ENGINE_DIR = Path(__file__).resolve().parent.parent
if str(ENGINE_DIR) not in sys.path:
    sys.path.insert(0, str(ENGINE_DIR))
