package texts

// Проверка оформления внешнего документа (.docx) по правилам Инструкции по ДОУ и ГОСТ Р 7.0.97-2025.
// Правила и пункты берутся из профилей internal/layout/formats, здесь - только тексты замечаний.
// {…} заменяются значениями из документа

const (
	FormatLevelError  = "error"
	FormatLevelRemark = "remark"

	FormatPageSize        = "Формат листа {width}×{height} мм, нужен A4 ({needWidth}×{needHeight} мм)"
	FormatMarginLeft      = "Левое поле {value} мм, нужно не менее {need} мм"
	FormatMarginRight     = "Правое поле {value} мм, нужно не менее {need} мм"
	FormatMarginTop       = "Верхнее поле {value} мм, нужно не менее {need} мм"
	FormatMarginBottom    = "Нижнее поле {value} мм, нужно не менее {need} мм"
	FormatFontName        = "Шрифт {fonts} вместо {need} (абзацев: {count}), например: «{example}»"
	FormatFontSize        = "Размер шрифта {sizes} пт в основном тексте, нужно {need} пт (абзацев: {count}), например: «{example}»"
	FormatLineSpacing     = "Межстрочный интервал {values} в основном тексте, нужно от {min} до {max} (абзацев: {count}), например: «{example}»"
	FormatAlign           = "Текст не выровнен по ширине (абзацев: {count}), например: «{example}»"
	FormatIndent          = "Абзацный отступ {values} мм вместо {need} мм (абзацев: {count}), например: «{example}»"
	FormatPageNumberNone  = "Нет номера страницы. Если в документе больше одной страницы, номер нужен"
	FormatPageNumberPart  = "Номер страницы в {actual} колонтитуле, нужно в {need}"
	FormatPageNumberAlign = "Номер страницы выровнен {actual}, нужно {need}"
	FormatPageNumberFirst = "Номер страницы стоит и на первой странице, нумеруются вторая и последующие"
	FormatDateQuoted      = "Дата с днем в кавычках: «{example}», нужен формат {need}"
	FormatDateShort       = "День или месяц даты одной цифрой: «{example}», нужен формат {need}"
	FormatInitialsTight   = "Инициалы без пробела: «{example}», по правилам - через пробел, «И. И. Иванов»"
	FormatInitialsSpaced  = "Пробел между инициалами: «{example}», по правилам - «И.И. Иванов»"
	FormatTemplateLines   = "Остались линии для заполнения: «{example}»"
	FormatTemplateMarks   = "Остались номера полей шаблона: «{example}»"
	FormatTemplateRed     = "Остался красный текст шаблона: «{example}»"
	FormatTemplateModule  = "Не удалена пометка «МОДУЛЬНЫЙ ДОКУМЕНТ»"

	// Пункт для замечаний по остаткам шаблона: правила нет в профилях оформления
	FormatTemplateProfile = "Методические рекомендации по работе с модульными документами"
	FormatTemplateRef     = "п. 3.3: красный текст, номера полей и пометки модуля удаляются из готового документа"

	FormatPartHeader  = "верхнем"
	FormatPartFooter  = "нижнем"
	FormatAlignLeft   = "по левому краю"
	FormatAlignRight  = "по правому краю"
	FormatAlignCenter = "по центру"
	FormatModuleMark  = "МОДУЛЬНЫЙ ДОКУМЕНТ"
	// Пример текста в замечании не длиннее стольких символов
	FormatExampleRunes = 70
	// Основной текст - абзацы не короче стольких символов: реквизиты (адресат, подпись) короче и проверяются отдельно
	FormatBodyParagraphRunes = 100
	// Сколько символов текста примерно на странице: без номера страницы короткий документ замечания не получает
	FormatPageRunes = 2500
)

// Реквизиты: нет заголовка или подписи
const (
	FormatTitleMissing     = "Нет заголовка к тексту: кратко, о чем документ, с предлогом «О» или «Об»"
	FormatSignatureMissing = "Нет подписи: должность и инициалы с фамилией того, кто подписывает документ"
	FormatTitleProfile     = "ГОСТ Р 7.0.97-2025"
	FormatTitleRef         = "п. 5.17: заголовок к тексту отвечает на вопрос «о чем?»"
)

const (
	// По этому слову в названии профиля замечание связывается с файлом Инструкции в базе знаний
	FormatInstructionMark = "инструкц"
)

// Что исправлено в оформлении после подтверждения сотрудника
const (
	FormatFixPage            = "Формат листа приведен к A4"
	FormatFixMargins         = "Поля приведены к правилам"
	FormatFixFirstPage       = "Номер страницы убран с первой страницы"
	FormatFixFont            = "Шрифт заменен на Times New Roman"
	FormatFixSize            = "Размер шрифта основного текста приведен к правилам"
	FormatFixSpacing         = "Межстрочный интервал основного текста приведен к правилам"
	FormatFixAlign           = "Основной текст выровнен по ширине"
	FormatFixIndent          = "Абзацный отступ приведен к 1,25 см"
	FormatFixPageNumber      = "Номер страницы выровнен по правилам"
	FormatFixDates           = "Даты записаны в формате ДД.ММ.ГГГГ"
	FormatFixInitials        = "Инициалы записаны по правилам"
	FormatFixAddressee       = "Добавлен адресат"
	FormatFixTitle           = "Добавлен заголовок к тексту"
	FormatFixSignature       = "Добавлена подпись"
	FormatFixPageNumberAdded = "Добавлен номер страницы"
	// Имя исправленного файла: имя исходного и эта приписка
	FormatFixFileSuffix = " - оформление исправлено.docx"
)
