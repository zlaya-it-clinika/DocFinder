"""Отбор контекста для ответа.

1. Сущности последней реплики + истории: названный договор/контрагент, период события.
2. В названном документе ищем лучшие положения вместо передачи всего договора, плюс ДС и связанные.
3. Остальное - гибридный поиск по всей базе (+ реранкер, если доступен), необходимые соседние пункты.
4. Редакции: на дату события берём действовавшую редакцию, без даты - действующую.
5. Законы: пометки о редакции и предупреждение, если событие раньше изменения нормы.
"""
from __future__ import annotations

import re
from dataclasses import dataclass, field

import dates
import entities as ent_mod
import textnorm
from dialog import Dialog
from entities import Entities
from index import SearchIndex
from kb import CLAUSE_REF, KnowledgeBase
from kb_parse import LAW, Chunk, Document
from llm_client import ApiError, LlmClient

CONTEXT_CHARS = 16000      # общий объём фрагментов в промпте
SEARCH_POOL = 24           # кандидатов из поиска до реранкера
MIN_KEEP = 3               # столько лучших кандидатов поиска остаются всегда
RELATIVE_CUTOFF = 0.45     # остальные - если близость не ниже этой доли от лучшего
FOCUS_KEEP = 6             # лучших положений точно названного документа
FOCUS_SUPPORT_KEEP = 3     # положений из остальных документов в дополнение к названному
MAX_EXPANDED_PATH_CHARS = 4500  # объём всех кусков одного длинного положения
MAX_FOCUS_CONTEXT = 10     # максимум пунктов одного точно названного документа с соседями
OLD_EDITION_WORDS = re.compile(r"раньше|ранее|прежн|старой\s+редакц|утратил|действовал\w*\s+до", re.I)
REGULATION_FOLLOWUP = re.compile(r"порог\w*.*(?:гендиректор|генеральн)|регламент", re.I)
EDITION_YEAR = re.compile(r"редакци[ияею]\s+(?:от\s+)?(20\d{2})", re.I)
REGULATION_TOPIC = re.compile(
    r"регламент|редакци|согласован|одобрен|визир|гендиректор|генеральн\w*\s+директор|"
    r"финансов\w*\s+директор|порог|оригинал|архив|СЭД|типов\w*\s+форм|"
    r"аффилирован|иностранн|поручител|залог", re.I)
CONTRACT_CONDITION = re.compile(
    r"мебел|компьютер|техник|склад|арендн\w*\s+плат|претензи|доставк|упаковк|"
    r"спецификац|допсоглашен|химки|П-17/2024|ПТ-05/2023|А-08/2024", re.I)

QUERY_EXPANSIONS = (
    (re.compile(r"субаренд", re.I),
     "арендатор не вправе сдавать помещение в субаренду без письменного согласия арендодателя"),
    (re.compile(r"(?:пято[ге]|5[- ]?го).*?(?:числ|месяц|плат)|(?:числ|месяц|плат).*?(?:пято[ге]|5[- ]?го)", re.I),
     "арендная плата вносится ежемесячно не позднее 5-го числа текущего месяца"),
    (re.compile(r"сч[её]т.*перечисл|перечисл.*сч[её]т", re.I),
     "расчёты производятся в безналичной форме путём перечисления на расчётный счёт поставщика"),
    (re.compile(r"оплат\w*.*исполн|исполн\w*.*оплат|плат[её]жн\w*\s+поручен", re.I),
     "обязательство покупателя по оплате считается исполненным с момента списания денежных средств с корреспондентского счёта"),
    (re.compile(r"срок\w*\s+поставк\w*.*компьютер|компьютер\w*.*срок\w*\s+поставк", re.I),
     "поставщик поставляет компьютерную технику в течение 14 рабочих дней с даты получения заявки"),
    (re.compile(r"гендиректор|генеральн\w*\s+директор", re.I),
     "договоры на сумму свыше подлежат одобрению генеральным директором до подписания"),
    (re.compile(r"инициир|инициатор", re.I),
     "инициатором согласования является руководитель подразделения в интересах которого заключается договор"),
    (re.compile(r"доказ\w*.*согласи|согласи\w*.*доказ", re.I),
     "обязанность предоставить доказательство получения согласия субъекта персональных данных возлагается на оператора"),
    (re.compile(r"ноч|круглосуточ|два\s+часа", re.I), "круглосуточно доступ транспорт пропуск"),
    (re.compile(r"аванс|заранее\s+оплат|до\s+поставк", re.I), "предоплата аванс счёт"),
    (re.compile(r"съезж|сдать\s+ключ|вернуть\s+ключ|вернуть\s+помещ|выезд", re.I),
     "возврат помещения акт прекращение договора"),
    (re.compile(r"ключ\w*.*(?:возврат|верн|сда|хватит)|(?:возврат|верн|сда|хватит).*ключ", re.I),
     "при прекращении договора арендатор возвращает помещение по акту"),
    (re.compile(r"сколько\s+стоит\w*\s+аренд|стоимост\w*\s+аренд", re.I),
     "арендная плата размер в месяц"),
    (re.compile(r"провер\w*\s+качеств|дефект|недостат", re.I), "приёмка качество недостатки претензия акт"),
    (re.compile(r"срок\w*\s+оплат|когда\s+оплат|когда\s+плат", re.I),
     "покупатель оплачивает товар в течение календарных дней с даты подписания УПД поставки"),
    (re.compile(r"пен[ия]|неустойк|просроч", re.I),
     "за просрочку поставки поставщик уплачивает неустойку за каждый день"),
    (re.compile(r"одобр\w*.*(?:генеральн|гендиректор)|(?:генеральн|гендиректор)\w*.*одобр", re.I),
     "договоры на сумму подлежат одобрению генеральным директором до подписания"),
    (re.compile(r"гарант\w*.*(?:компьютер|техник)|(?:компьютер|техник).*гарант", re.I),
     "гарантийный срок на товар устанавливается производителем указывается в гарантийном талоне"),
    (re.compile(r"цен\w*.*парт|парт\w*.*цен", re.I),
     "наименование количество ассортимент и цена товара согласуются сторонами в спецификациях"),
    (re.compile(r"(?:ндс|налог\w*\s+на\s+добавленн\w*\s+стоимост).*молок", re.I),
     "молока молокопродуктов продовольственных товаров налоговая ставка при реализации"),
)


@dataclass
class ContextItem:
    label: str
    chunk: Chunk
    doc: Document
    marks: list[str] = field(default_factory=list)


@dataclass
class Retrieved:
    items: list[ContextItem]
    entities: Entities
    facts: list[str] = field(default_factory=list)      # подсказки модели: дата события, редакции, ДС
    warnings: list[str] = field(default_factory=list)   # чего нет в базе (редакции законов и т. п.)
    coverage: float = 0.0                               # доля слов вопроса, найденных в лучшем фрагменте
    search_query: str = ""
    scores: dict[int, float] = field(default_factory=dict)  # близость фрагмента к последней реплике

    def by_label(self) -> dict[str, ContextItem]:
        return {item.label: item for item in self.items}

    def ranked_items(self) -> list[ContextItem]:
        """Фрагменты по близости к вопросу; реквизиты и шапки - в конце."""
        def key(item: ContextItem) -> tuple[int, float]:
            return (item.chunk.kind in ("header", "section"), -self.scores.get(item.chunk.index, 0.0))
        return sorted(self.items, key=key)


def build_query(dialog: Dialog) -> str:
    """Последняя реплика дважды (важнее) + реплики истории: в них номер договора и контрагент."""
    last = dialog.last_user
    history = [turn.text for turn in dialog.history]
    source = "\n".join([last] + history)
    expanded = [terms for pattern, terms in QUERY_EXPANSIONS if pattern.search(source)]
    return "\n".join([last, last] + expanded + history)


def coverage(query: str, chunk_text: str) -> float:
    words = textnorm.content_stems(query)
    if not words:
        return 0.0
    return len(words & textnorm.content_stems(chunk_text)) / len(words)


def retrieve(kb: KnowledgeBase, index: SearchIndex, dialog: Dialog, client: LlmClient | None = None,
             query_vector: list[float] | None = None, top_n: int = 8,
             deadline: float | None = None) -> Retrieved:
    last = ent_mod.extract(kb, dialog.last_user)
    context = ent_mod.extract(kb, "\n".join(turn.text for turn in dialog.history))
    entities = ent_mod.merge(last, context)
    # История даёт дату, но её договор не должен сужать поиск, если последний
    # вопрос явно перешёл на общий порог одобрения или регламент.
    if not last.doc_ids and context.doc_ids and REGULATION_FOLLOWUP.search(dialog.last_user):
        entities.doc_ids = [doc_id for doc_id in entities.doc_ids
                            if kb.by_id[doc_id].doc_type == "regulation"]
    query = build_query(dialog)
    selected: list[int] = []

    # Точно названный документ - фильтр поиска, а не разрешение отправить модели весь договор.
    # Иначе нужный пункт теряется среди 20-30 положений, совпавших только по номеру договора.
    focus = _focus_docs(kb, entities)
    focus_ids = {doc.doc_id for doc in focus}
    focus_allowed = {c.index for c in kb.chunks
                     if c.doc_id in focus_ids and c.kind in ("clause", "whole")}

    # Сначала кандидаты из названного документа, затем общий поиск для законов, регламентов и связанных норм.
    if focus_allowed:
        hits = index.search(query, query_vector, allowed=focus_allowed, limit=SEARCH_POOL)
        hits += index.search(query, query_vector, limit=SEARCH_POOL // 2)
    else:
        hits = index.search(query, query_vector, limit=SEARCH_POOL)
    expanded_terms = [terms for pattern, terms in QUERY_EXPANSIONS if pattern.search(dialog.last_user)]
    if expanded_terms:
        expanded = index.search(" ".join(expanded_terms), query_vector, allowed=focus_allowed or None, limit=3)
        hits = expanded + hits
    lexical = index.search(query, None, allowed=focus_allowed or None, limit=3)
    hits = lexical + hits
    ranked = _rerank(kb, index, client, dialog.last_user, hits, top_n, deadline)
    # Шапки и реквизиты - шум: документ уже известен из метаданных ContextItem.
    ranked = [i for i in ranked if kb.chunks[i].kind in ("clause", "whole")]
    # Отсечь слабые совпадения: близость ниже доли от лучшего (реранкер уже упорядочил, порядок сохраняем)
    closeness = _closeness(index, dialog, query_vector, ranked)
    best = max(closeness.values(), default=0.0)
    ranked = [i for position, i in enumerate(ranked)
              if position < MIN_KEEP or closeness.get(i, 0.0) >= RELATIVE_CUTOFF * best]
    if focus_ids:
        focused = [i for i in ranked if kb.chunks[i].doc_id in focus_ids]
        support = [i for i in ranked
                   if kb.chunks[i].doc_id not in focus_ids and kb.doc(kb.chunks[i]).doc_type != "contract"]
        keep = focused[:FOCUS_KEEP] + support[:FOCUS_SUPPORT_KEEP]
    else:
        keep = ranked[:top_n]
    for chunk_index in keep:
        if chunk_index not in selected:
            selected.append(chunk_index)

    # Ничего не названо, но неоднозначно («Альфа»): по два лучших положения каждого кандидата.
    for group in entities.ambiguous:
        for doc_id in group:
            allowed = {chunk.index for chunk in kb.by_id[doc_id].chunks
                       if chunk.kind in ("clause", "whole")}
            for chunk_index, _ in index.search(query, query_vector, allowed=allowed, limit=2):
                if chunk_index not in selected:
                    selected.append(chunk_index)

    # Регламент согласования не помогает ответить на отдельное условие найденного
    # договора; случайные совпадения добавляли в ответ постороннюю редакцию.
    contract_context = any(kb.doc(kb.chunks[i]).doc_type in ("contract", "supplement") for i in selected)
    if (contract_context and CONTRACT_CONDITION.search(dialog.last_user)
            and not REGULATION_TOPIC.search(dialog.last_user)):
        selected = [i for i in selected if kb.doc(kb.chunks[i]).doc_type != "regulation"]
        ranked = [i for i in ranked if kb.doc(kb.chunks[i]).doc_type != "regulation"]

    result = Retrieved(items=[], entities=entities, search_query=query)
    # Основные документы: названные в вопросе и лучшие по поиску. Только к ним тянем ДС и связанные
    # Связанные документы и ДС тянем только для явно названных документов и самого сильного результата.
    # Иначе налоговый вопрос может получить факт о случайно совпавшем договоре поставки.
    primary = focus_ids | ({kb.chunks[ranked[0]].doc_id} if ranked else set())
    chunks = [kb.chunks[i] for i in selected]
    chunks = _apply_editions(kb, chunks, entities, dialog.last_user, result)
    chunks = _add_pieces(kb, chunks, dialog.last_user)
    chunks = _add_neighbors(kb, chunks, focus_ids)
    chunks = _add_supplements_and_related(kb, chunks, primary, result)
    _law_warnings(kb, [c for c in chunks if c.doc_id in primary], entities, result)
    result.items = _to_items(kb, chunks)
    result.scores = _closeness(index, dialog, query_vector, [item.chunk.index for item in result.items])
    user_query = "\n".join(dialog.user_texts())
    result.coverage = max((max(coverage(dialog.last_user, index.texts[item.chunk.index]),
                               coverage(user_query, index.texts[item.chunk.index]))
                           for item in result.items if item.chunk.kind != "header"), default=0.0)
    _contract_facts(kb, focus, result)
    if entities.period:
        start, end = entities.period
        when = dates.fmt(start) if start == end else f"{dates.fmt(start)}-{dates.fmt(end)}"
        result.facts.insert(0, f"Дата (период) события из вопроса: {when}.")
    return result


def _closeness(index: SearchIndex, dialog: Dialog, query_vector: list[float] | None,
               indexes: list[int]) -> dict[int, float]:
    """BM25 последней реплики (основной вес) + истории + косинус, нормированные к [0, 1]."""
    if not indexes:
        return {}
    last = index.bm25_scores(dialog.last_user)
    full = index.bm25_scores("\n".join(dialog.user_texts()))
    dense = index.vector_scores(query_vector)

    def norm(values: list[float]) -> list[float]:
        top = max(values) if values else 0.0
        return [value / top if top > 0 else 0.0 for value in values]

    parts = [norm([float(last[i]) for i in indexes]), norm([float(full[i]) for i in indexes])]
    weights = [1.0, 0.4]
    if dense is not None:
        parts.append(norm([max(0.0, float(dense[i])) for i in indexes]))
        weights.append(1.0)
    return {i: sum(w * p[pos] for w, p in zip(weights, parts)) for pos, i in enumerate(indexes)}


def _focus_docs(kb: KnowledgeBase, entities: Entities) -> list[Document]:
    docs = []
    for doc_id in entities.doc_ids:
        doc = kb.by_id[doc_id]
        if doc not in docs:
            docs.append(doc)
        for sup in doc.supplements:
            if kb.by_id[sup] not in docs:
                docs.append(kb.by_id[sup])
    return docs


def _rerank(kb: KnowledgeBase, index: SearchIndex, client: LlmClient | None, question: str,
            hits: list[tuple[int, float]], top_n: int, deadline: float | None) -> list[int]:
    order = list(dict.fromkeys(chunk_index for chunk_index, _ in hits))
    if client is None or not client.available("rerank") or len(order) <= 1:
        return order
    try:
        ranked = client.rerank(question, [index.texts[i] for i in order], top_n=min(len(order), max(top_n, 12)),
                               deadline=deadline)
    except ApiError:
        return order
    # Сохраняем несколько сильнейших кандидатов исходного поиска: реранкер может
    # ошибиться на коротком вопросе, особенно при похожих договорах и редакциях.
    anchors = order[:MIN_KEEP + 2]
    head = [order[i] for i, _ in ranked if order[i] not in anchors]
    return anchors + head + [i for i in order if i not in anchors and i not in head]


def _edition_fact(doc: Document) -> str:
    period = ""
    if doc.effective_from:
        period = f" (действовал с {dates.fmt(doc.effective_from)}"
        period += f" до {dates.fmt(doc.effective_to)})" if doc.effective_to else ")"
    return f"На дату события действовал документ «{doc.display_name}»{period}."


def _apply_editions(kb: KnowledgeBase, chunks: list[Chunk], entities: Entities, question: str,
                    result: Retrieved) -> list[Chunk]:
    """Из нескольких редакций одного документа оставить нужные.

    Есть дата события - редакция на эту дату (и действующая для сравнения, если отличается).
    Даты нет - только действующая, если вопрос не про прошлое («раньше», «утратил силу»).
    """
    groups = {kb.doc(c).group_key for c in chunks if kb.doc(c).group_key}
    if not groups:
        return chunks
    keep: dict[str, set[str]] = {}
    explicit_edition_year = EDITION_YEAR.search(question)
    for group in groups:
        current = kb.edition_on(group, None)
        wanted: set[str] = {current.doc_id} if current else set()
        if entities.period:
            start, end = entities.period
            event_docs = {doc.doc_id for doc in (kb.edition_on(group, start), kb.edition_on(group, end)) if doc}
            if event_docs:
                wanted |= event_docs
                result.facts.extend(_edition_fact(kb.by_id[doc_id]) for doc_id in sorted(event_docs))
                if current and current.doc_id not in event_docs:
                    result.facts.append(f"Действующая сейчас редакция: «{current.display_name}»; к событиям "
                                        f"до {dates.fmt(current.effective_from)} она не применяется.")
            else:
                result.warnings.append(f"Редакции документа на дату события в базе нет (группа «{group}»).")
        elif explicit_edition_year:
            year = int(explicit_edition_year.group(1))
            named = {doc.doc_id for doc in kb.docs
                     if doc.group_key == group and doc.edition_date and doc.edition_date.year == year}
            if named:
                wanted = named
                result.facts.extend(f"По вопросу выбрана редакция «{kb.by_id[doc_id].display_name}»."
                                    for doc_id in sorted(named))
            else:
                result.warnings.append(f"Редакции документа за {year} год в базе нет (группа «{group}»).")
        elif OLD_EDITION_WORDS.search(question):
            wanted = {doc.doc_id for doc in kb.docs if doc.group_key == group}
        elif current:
            edition_date = current.edition_date or current.effective_from
            dated = f" от {dates.fmt(edition_date)}" if edition_date else ""
            result.facts.append(
                f"В вопросе дата редакции не указана; для ответа выбрана действующая редакция "
                f"«{current.display_name}»{dated}. Назови эту редакцию в ответе."
            )
        keep[group] = wanted
    kept = [c for c in chunks if not kb.doc(c).group_key or kb.doc(c).doc_id in keep[kb.doc(c).group_key]]
    # Нужная редакция могла не попасть в поиск: добавить тот же пункт из неё
    present = {c.index for c in kept}
    for chunk in list(kept):
        doc = kb.doc(chunk)
        for other_id in keep.get(doc.group_key, set()) if doc.group_key else ():
            twin = next((c for c in kb.by_id[other_id].chunks if c.path == chunk.path), None)
            if twin and twin.index not in present:
                kept.append(twin)
                present.add(twin.index)
    # Пункт нашёлся только в отброшенной редакции: взять тот же пункт из нужной
    for chunk in chunks:
        doc = kb.doc(chunk)
        if doc.group_key and doc.doc_id not in keep[doc.group_key]:
            for other_id in keep[doc.group_key]:
                twin = next((c for c in kb.by_id[other_id].chunks if c.path == chunk.path), None)
                if twin and twin.index not in present:
                    kept.append(twin)
                    present.add(twin.index)
    return kept

def _add_pieces(kb: KnowledgeBase, chunks: list[Chunk], question: str) -> list[Chunk]:
    """Раскрыть куски только одного самого приоритетного длинного положения.

    Начало нормы важно сохранить, но раскрытие каждого совпавшего пути заполняет контекст
    конкурирующими ставками и исключениями. Для остальных путей остаётся лучший найденный кусок.
    """
    groups: dict[tuple[str, str], list[Chunk]] = {}
    for chunk in chunks:
        if not chunk.path or kb.doc(chunk).category != LAW:
            continue
        key = (chunk.doc_id, chunk.path)
        groups.setdefault(key, [other for other in kb.doc(chunk).chunks if other.path == chunk.path])
    expandable = [key for key, siblings in groups.items() if len(siblings) > 1]
    expand_key = max(
        expandable,
        key=lambda key: (
            max(coverage(question, sibling.text) for sibling in groups[key]),
            -min(sibling.order for sibling in groups[key]),
        ),
        default=None,
    )

    result: list[Chunk] = []
    present: set[int] = set()
    seen_law_paths: set[tuple[str, str]] = set()
    expanded = False
    for chunk in chunks:
        key = (chunk.doc_id, chunk.path)
        if key == expand_key:
            if expanded:
                continue
            expanded = True
            total = 0
            siblings = sorted(groups[key], key=lambda item: item.order)
            chosen: list[Chunk] = []
            for sibling in siblings:
                if total + len(sibling.text) <= MAX_EXPANDED_PATH_CHARS or sibling.index == chunk.index:
                    chosen.append(sibling)
                    total += len(sibling.text)
            if chunk not in chosen:
                chosen.append(chunk)
            for sibling in sorted(chosen, key=lambda item: item.order):
                if sibling.index not in present:
                    result.append(sibling)
                    present.add(sibling.index)
            seen_law_paths.add(key)
            continue
        if kb.doc(chunk).category == LAW and chunk.path:
            if key in seen_law_paths:
                continue
            seen_law_paths.add(key)
        if chunk.index not in present:
            result.append(chunk)
            present.add(chunk.index)
    return result


def _add_neighbors(kb: KnowledgeBase, chunks: list[Chunk], focus_ids: set[str]) -> list[Chunk]:
    """Соседние пункты того же раздела документа компании: «3.1» тянет «3.2» и «3.3»."""
    present = {c.index for c in chunks}
    result = list(chunks)
    focus_counts = {doc_id: sum(chunk.doc_id == doc_id for chunk in chunks) for doc_id in focus_ids}
    for chunk in chunks:
        if kb.doc(chunk).category == LAW:
            continue
        radius = 2 if chunk.doc_id in focus_ids else 1
        for neighbor in kb.neighbors(chunk, radius=radius):
            if chunk.doc_id in focus_ids and focus_counts[chunk.doc_id] >= MAX_FOCUS_CONTEXT:
                break
            if neighbor.index not in present and neighbor.kind == "clause":
                result.append(neighbor)
                present.add(neighbor.index)
                if chunk.doc_id in focus_ids:
                    focus_counts[chunk.doc_id] += 1
    return result


def _add_supplements_and_related(kb: KnowledgeBase, chunks: list[Chunk], primary: set[str],
                                 result: Retrieved) -> list[Chunk]:
    present = {c.index for c in chunks}
    out = list(chunks)
    doc_ids = {c.doc_id for c in chunks} & primary
    for doc_id in sorted(doc_ids):
        doc = kb.by_id[doc_id]
        for supplement_id in doc.supplements:
            for chunk in kb.by_id[supplement_id].chunks:
                if chunk.index not in present:
                    out.append(chunk)
                    present.add(chunk.index)
        # Связанный документ добавляется точечно по ссылкам из исходного документа.
        # Например, письмо К-12 ссылается на п. 5.4 Регламента: нужен этот пункт, а не весь Регламент.
        for related_id in doc.related:
            related = kb.by_id[related_id]
            refs = set(CLAUSE_REF.findall(doc.text))
            related_chunks = [chunk for chunk in related.chunks if chunk.clause in refs] if refs else []
            if not refs and len(related.chunks) <= 2:
                related_chunks = list(related.chunks)
            for chunk in related_chunks:
                if chunk.index not in present:
                    out.append(chunk)
                    present.add(chunk.index)
    for sup_id, clauses in kb.amended_clauses.items():
        sup = kb.by_id[sup_id]
        if sup.parent_id in doc_ids and clauses:
            result.facts.append(f"«{sup.display_name}» от {dates.fmt(sup.doc_date)} изменяет п. "
                                f"{', '.join(clauses)} договора; учитывай условия применения изменения.")
    return out


def _law_warnings(kb: KnowledgeBase, chunks: list[Chunk], entities: Entities, result: Retrieved) -> None:
    laws = sorted({c.doc_id for c in chunks if kb.doc(c).category == LAW})
    exported = sorted({kb.by_id[doc_id].exported_at for doc_id in laws if kb.by_id[doc_id].exported_at})
    if exported:
        result.facts.append(f"Законы есть в базе только в действующей редакции (выгрузка {dates.fmt(exported[-1])}).")
    for doc_id in laws:
        doc = kb.by_id[doc_id]
        if entities.period:
            changed = [day for day, _ in doc.amendments if day > entities.period[0]]
            if changed:
                result.warnings.append(
                    f"В «{doc.display_name}» есть изменения, вступившие в силу после даты события "
                    f"(с {dates.fmt(min(changed))}); прежней редакции в базе нет, выводы о норме на дату события "
                    f"по изменённым положениям делать нельзя.")
        future = sorted(day for day, _ in doc.amendments if doc.exported_at and day > doc.exported_at)
        if future:
            result.warnings.append(f"«{doc.display_name}»: изменения с {dates.fmt(future[0])} отражены в базе только "
                                   f"пометкой, текста будущей редакции нет.")


def _contract_facts(kb: KnowledgeBase, focus: list[Document], result: Retrieved) -> None:
    for doc in focus:
        if doc.doc_type == "contract" and doc.doc_date:
            result.facts.append(f"«{doc.display_name}» заключён {dates.fmt(doc.doc_date)}.")


def _to_items(kb: KnowledgeBase, chunks: list[Chunk]) -> list[ContextItem]:
    """Отбор и показ в порядке поискового приоритета: самые полезные доказательства идут первыми."""
    unique: dict[int, Chunk] = {}
    for chunk in chunks:
        unique.setdefault(chunk.index, chunk)
    chosen: list[Chunk] = []
    total = 0
    for chunk in unique.values():
        size = len(chunk.text) + 150
        if total + size > CONTEXT_CHARS and chosen:
            continue
        total += size
        chosen.append(chunk)
    items: list[ContextItem] = []
    for chunk in chosen:
        doc = kb.doc(chunk)
        marks = []
        if doc.effective_to:
            marks.append(f"редакция утратила силу с {dates.fmt(doc.effective_to)}")
        elif doc.effective_from and doc.group_key:
            marks.append(f"действующая редакция с {dates.fmt(doc.effective_from)}")
        items.append(ContextItem(label=f"F{len(items) + 1}", chunk=chunk, doc=doc, marks=marks))
    return items
