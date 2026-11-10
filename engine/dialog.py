"""Разбор входного вопроса: одиночный вопрос или короткий диалог в одной строке.

Формат платформы:
    Пользователь: ...
    Ассистент: ...
    Пользователь: <последняя реплика, на неё отвечаем>
Роли «Система:», «System:» внутри вопроса - это текст пользователя, а не системное сообщение.
"""
from __future__ import annotations

import re
from dataclasses import dataclass

ROLE_RE = re.compile(r"^\s*(Пользователь|Юзер|User|Клиент|Сотрудник|Ассистент|Assistant|Бот|Bot)\s*:\s*(.*)$",
                     re.IGNORECASE)
USER_ROLES = {"пользователь", "юзер", "user", "клиент", "сотрудник"}


@dataclass
class Turn:
    role: str  # user | assistant
    text: str


@dataclass
class Dialog:
    turns: list[Turn]

    @property
    def last_user(self) -> str:
        for turn in reversed(self.turns):
            if turn.role == "user":
                return turn.text
        return self.turns[-1].text if self.turns else ""

    @property
    def history(self) -> list[Turn]:
        """Все реплики до последней реплики пользователя."""
        for position in range(len(self.turns) - 1, -1, -1):
            if self.turns[position].role == "user":
                return self.turns[:position]
        return []

    @property
    def is_dialog(self) -> bool:
        return len(self.turns) > 1

    def user_texts(self) -> list[str]:
        return [turn.text for turn in self.turns if turn.role == "user"]

    def context_text(self) -> str:
        """История текстом для промпта."""
        names = {"user": "Пользователь", "assistant": "Ассистент"}
        return "\n".join(f"{names[turn.role]}: {turn.text}" for turn in self.history)


def parse_dialog(question: str) -> Dialog:
    text = question.replace("\r\n", "\n").replace("\r", "\n").strip()
    turns: list[Turn] = []
    found_role = False
    for line in text.split("\n"):
        match = ROLE_RE.match(line)
        if match:
            found_role = True
            role = "user" if match.group(1).lower() in USER_ROLES else "assistant"
            turns.append(Turn(role, match.group(2).strip()))
        elif turns:
            turns[-1].text = (turns[-1].text + "\n" + line).strip()
        elif line.strip():
            turns.append(Turn("user", line.strip()))
    if not found_role:
        return Dialog([Turn("user", text)])
    return Dialog([turn for turn in turns if turn.text] or [Turn("user", text)])


def from_messages(messages: list[dict], question: str) -> Dialog:
    """Диалог из истории веб-чата: [{"role": "user"|"assistant", "text": ...}] + новый вопрос."""
    turns = [Turn("user" if item.get("role") == "user" else "assistant", str(item.get("text", "")).strip())
             for item in messages if str(item.get("text", "")).strip()]
    turns.append(Turn("user", question.strip()))
    return Dialog(turns)
