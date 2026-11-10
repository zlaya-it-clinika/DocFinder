package texts

const (
	KindAnswer        = "answer"
	KindClarify       = "clarify"
	KindFallback      = "fallback"
	KindTemplateField = "template_field"
	KindTemplateReady = "template_ready"
	KindAccessDenied  = "access_denied"
	KindDraftRevision = "draft_revision"
	// Виды ответа движка юридического ассистента (engine/pipeline.py)
	KindPartial    = "partial"
	KindRefusal    = "refusal"
	KindCorrection = "correction"
	KindIllegal    = "illegal"
	KindOutOfScope = "out_of_scope"

	RoleAdmin   = "admin"
	RoleUser    = "user"
	RolePending = "pending"

	RoleIDChief          = "role-chief"
	RoleTitleChief       = "Главный администратор"
	RoleDescriptionChief = "Системная роль со всеми флагами"
	RoleIDAdmin          = "role-admin"
	RoleTitleAdmin       = "Администратор"
	RoleDescriptionAdmin = "Все флаги, кроме управления ролями"
	RoleIDUser           = "role-user"
	RoleTitleUser        = "Сотрудник"
	RoleDescriptionUser  = "Вопросы, свои документы, бланки и проверка черновика"

	PermissionChatUse            = "chat.use"
	PermissionPersonalUpload     = "documents.personal.upload"
	PermissionGlobalManage       = "documents.global.manage"
	PermissionPersonalViewAll    = "documents.personal.view_all"
	PermissionDraftsCheck        = "drafts.check"
	PermissionDraftsAssist       = "drafts.assist"
	PermissionTemplatesUse       = "templates.use"
	PermissionUsersManage        = "users.manage"
	PermissionRolesManage        = "roles.manage"
	PermissionAuditView          = "audit.view"
	PermissionAIManage           = "ai.manage"
	PermissionOrganizationManage = "organization.manage"
	PermissionSourcesManage      = "sources.manage"
	PermissionActionAssign       = "assign_role"
	PermissionActionRevoke       = "revoke_role"
	PermissionActionUpdate       = "update_role"
	PermissionActionDeleteUser   = "delete_user"

	ScopeGlobal   = "global"
	ScopePersonal = "personal"
	ScopeAll      = "all"

	ProcessDocuments = "documents"
	// Несколько процессов в чате: коды и названия через разделитель
	ProcessSelectionSeparator = ","
	ProcessTitleSeparator     = ", "
	ProcessTitleDocuments     = "Документооборот"
	ProcessDefaultColor       = "gray"
	SeedProcessColor          = "blue"
	// Сколько названий документов процесса попадает в подсказку модели
	ProcessMapLimit = 40

	ErrorInvalidCredentials       = "invalid_credentials"
	ErrorUnauthorized             = "unauthorized"
	ErrorAccessDenied             = "access_denied"
	ErrorNotFound                 = "not_found"
	ErrorLoginTaken               = "login_taken"
	ErrorRoleInUse                = "role_in_use"
	ErrorInvalidBody              = "invalid_body"
	ErrorLiveNotReady             = "live_not_ready"
	ErrorEmbedSize                = "embed_size_mismatch"
	ErrorImageInvalid             = "image_invalid"
	ErrorEmbedUnavailable         = "embed_unavailable"
	ErrorStore                    = "store_unavailable"
	ErrorSignupDisabled           = "signup_disabled"
	ErrorAccountPending           = "account_pending"
	ErrorInvalidEmail             = "invalid_email"
	ErrorLdapDisabled             = "ldap_disabled"
	ErrorKerberosDisabled         = "kerberos_disabled"
	ErrorLdapUnavailable          = "ldap_unavailable"
	ErrorLdapEmailMissing         = "ldap_email_missing"
	ErrorDirectoryAccountConflict = "directory_account_conflict"
	ErrorOllamaUnavailable        = "ollama_unavailable"
	// Скачивание и удаление моделей - только у Ollama, не у OpenAI-совместимого API
	ErrorProviderNotSupported = "provider_not_supported"
	// Движок юридического ассистента (engine/server.py) не ответил
	ErrorEngineUnavailable   = "engine_unavailable"
	ErrorModelNotFound       = "model_not_found"
	ErrorModelInUse          = "model_in_use"
	ErrorPullRunning         = "pull_running"
	ErrorReindexRunning      = "reindex_running"
	ErrorSourceNotConfigured = "source_not_configured"
	ErrorSourceKindUnknown   = "source_kind_unknown"
	ErrorSourceLicense       = "source_license_denied"
	ErrorSourceUnavailable   = "source_unavailable"
	ErrorSourceSyncRunning   = "source_sync_running"
	ErrorStandardDocument    = "standard_document"

	DefaultLdapLabel = "LDAP"

	LiveDatabaseMissing = "Для API_MODE=live нужен DATABASE_URL."
	LiveDatabaseConnect = "Не удалось подключиться к PostgreSQL."
	LiveEmbedDimensions = "OLLAMA_EMBED_DIMENSIONS должен быть положительным числом."
	// Стандартный набор документов: каталог из STANDARD_DOCS_DIR не читается
	LiveStandardDocsMissing = "Не найден каталог стандартного набора документов STANDARD_DOCS_DIR"

	DocsWrongPassword  = "Неверный пароль."
	DocsPasswordNotSet = "В .env не задан DOCS_PASSWORD."
	DocsBadForm        = "Не удалось прочитать форму."

	DocsLoginTitle    = "Вход в документацию API"
	DocsHeading       = "Документация API"
	DocsLoginLead     = "Страница с запросами к локальному ядру. Пароль берется из DOCS_PASSWORD."
	DocsPasswordLabel = "Пароль"
	DocsLoginButton   = "Войти"
	DocsMissingHint   = "Добавьте пароль в .env рядом с сервером и перезапустите его."

	DocsConsoleTitle     = "DocFinder API"
	DocsConsoleLead      = "Запросы уходят на этот же сервер. В заглушке: user / user и admin / admin, если STUB_SEED_USERS не false. В live первая регистрация создает администратора."
	DocsLogoutButton     = "Выйти"
	DocsTokenLabel       = "Токен API, заголовок Authorization"
	DocsTokenPlaceholder = "Появится после POST /auth/login"
	DocsSendButton       = "Отправить"
	DocsFilePrefix       = "Файл, "
	DocsFileSuffix       = " байт"

	DocsHintLogin           = "Вход сотрудника. Токен из ответа подставится в поле выше."
	DocsHintLogout          = "Завершает токен. Дальше запросы с ним отвечают 401."
	DocsHintMe              = "Текущий пользователь. В ответе roles и итоговые permissions. Поле role по-прежнему pending, user или admin."
	DocsHintCreateSession   = "Новая сессия. Ее id подставится в запросы истории и сообщений."
	DocsHintListSessions    = "Свои сессии, сначала новые. Заголовок берется из первого вопроса. canRename и canDelete рядом со списком и у каждой сессии всегда true для своих сессий."
	DocsHintRenameSession   = "Новый заголовок. Пустой текст отвечает 400. Длинный обрезается до 60 символов. Чужая сессия - 403."
	DocsHintDeleteSession   = "Удаляет сессию, сообщения и вложения черновика. Ответ 204. Чужая сессия - 403."
	DocsHintHistory         = "История сессии, выбранный процесс processId и режим чата mode (read или plan)."
	DocsHintMessage         = "Ответ, уточнение, отказ, бланк или правка черновика. Необязательный processId ограничивает поиск одним процессом. Необязательный attachmentIds выбирает вложения этой сессии: в live ответ draft_revision, в заглушке fallback и файл не переписывается. Без идентификаторов правка в live продолжается с последней версии. «как подготовить документ» дает fallback без цитат и без модели. Необязательный mode сохраняется в сессии: read (по умолчанию) - только база знаний, на просьбу подготовить документ в ответе hint; plan - документ: макет или бланк модуля по словам, затем по выбору модели, иначе template_choice со списком, ответ - номер или название. В ответе и отказе есть contacts, в draft_revision его нет."
	DocsHintAttach          = "Вложение черновика .txt, .pdf или .docx. Нужен флаг drafts.assist. Ответ: attachmentId, fileName, characters. В поиск не попадает."
	DocsHintListDocuments   = "Документы, которые пользователь может видеть. Параметр scope: global, personal или all. all только у администратора. Параметр process сужает список."
	DocsHintUpload          = "Файл .txt, .pdf или .docx. Пустой scope: у флага documents.global.manage область global, иначе personal. roleIds ограничивает общий документ ролями. Пустой roleIds и accessLevel 1 - всем, accessLevel 2 - администратору и главному администратору. Пустой process - документооборот."
	DocsHintProcesses       = "Справочник процессов. Любой вошедший пользователь."
	DocsHintCreateProcess   = "Новый процесс. Нужен флаг users.manage. Пустые промпты копируются из документооборота."
	DocsHintImport          = "Каталог на машине API, один уровень. Поля path, process, edition, accessLevel, scope. Чужие расширения попадают в skipped."
	DocsHintFile            = "Файл оригинала. Чужой личный документ для сотрудника выглядит как отсутствующий."
	DocsHintDeleteDocument  = "Удаляет документ и фрагменты. Общий удаляет администратор, личный - владелец или администратор. Ответ 204. Документ стандартного набора не удаляется: 409 standard_document."
	DocsHintDraftFix        = "Исправление оформления .docx после подтверждения сотрудника: лист, поля, номер страницы, шрифт, размер, интервал, выравнивание, отступ, даты и инициалы. Поля формы signerId, addresseeId - должностные лица, title - заголовок: недостающие реквизиты. Ответ: draftLink на исправленный файл, applied - что исправлено, recommendations - что осталось вручную. Нужен флаг drafts.check, не .docx - 400."
	DocsHintEditions        = "Архив прежних редакций: новая редакция с другим содержимым переносит прежнюю сюда. Новые первыми, у каждой fileLink."
	DocsHintEditionFile     = "Файл архивной редакции. Права те же, что у документа."
	DocsHintBlankFile       = "Исходный модульный бланк. id берется из template.fileLink, например order-work-group."
	DocsHintDraftFile       = "Заполненный .docx бланка. Скачивает только владелец. id берется из template.draftLink."
	DocsHintUsers           = "Список учетных записей. Нужен флаг users.manage."
	DocsHintCreateUser      = "Создание учетной записи. Нужен флаг users.manage. role: pending, user или admin."
	DocsHintPatchUser       = "Имя, почта, пароль, role или одна роль из конструктора: roleId (или roleIds из одного элемента). Нужен флаг users.manage. Свою роль сменить нельзя. Последнего главного администратора снять нельзя."
	DocsHintDeleteUser      = "Удаляет учетную запись, ее токены, сессии с сообщениями и вложениями, черновики и личные документы. Общие документы и журнал вопросов остаются. Нужен флаг users.manage, главного администратора удаляет только обладатель roles.manage. Себя и последнего главного администратора удалить нельзя. Ответ 204."
	DocsHintAudit           = "Журнал вопросов. Нужен флаг audit.view."
	DocsHintPermissions     = "Каталог флагов: permission, title, description. Любой вошедший."
	DocsHintRoles           = "Список ролей. Нужен флаг roles.manage."
	DocsHintCreateRole      = "Новая роль: title, description, permissions, ldapGroup. Нужен флаг roles.manage."
	DocsHintPatchRole       = "Название, описание, флаги и ldapGroup. Системную роль главного администратора менять нельзя."
	DocsHintDeleteRole      = "Удаление роли. Системную роль удалить нельзя. Роль с пользователями отвечает role_in_use."
	DocsHintPermissionAudit = "Журнал назначения ролей и смены флагов. Нужен флаг audit.view. Новые записи сверху."
	DocsHintDraft           = "Проверка внешнего документа. Нужен флаг drafts.check. У .docx - оформление по Инструкции и ГОСТ: замечания с level error или remark и пунктом правила. Затем текст: нет приложения или подписи - рекомендация с цитатой. У .txt и .pdf formatChecked: false."

	DocsHintAuthConfig     = "Без токена. onboarding=true, пока нет ни одной учетной записи."
	DocsHintSignup         = "Регистрация по почте. Первая учетная запись становится администратором, остальные получают роль по умолчанию."
	DocsHintLdapLogin      = "Вход доменной учетной записью. Нужен включенный LDAP. Если включено управление группами, роли заменяются совпавшими CN."
	DocsHintKerberosLogin  = "Вход Windows. Заголовок Authorization: Negotiate и билет в поле токена. Флаг выключен - 403. Нет заголовка - 401 и WWW-Authenticate: Negotiate."
	DocsHintAdminDetails   = "Контакт администратора для экрана ожидания. Работает и для роли pending."
	DocsHintSignupConfig   = "Регистрация и роль по умолчанию. Нужен флаг users.manage."
	DocsHintSaveSignup     = "Изменить регистрацию. Можно передать часть полей."
	DocsHintLdapToggle     = "Включен ли вход через LDAP. Нужен уровень 2."
	DocsHintSaveLdapToggle = "Включить или выключить вход через LDAP."
	DocsHintLdapServer     = "Подключение к LDAP, группы каталога и Kerberos. Пароль служебной учетной записи в ответ не попадает."
	DocsHintSaveLdapServer = "Сохранить подключение. Пустой appDnPassword оставляет прежний пароль."

	DocsExampleLoginBody = `{
  "login": "user",
  "password": "user"
}`
	DocsExampleSignupBody = `{
  "email": "ivanov@corp.local",
  "password": "secret",
  "displayName": "Иванов Иван"
}`
	DocsExampleLdapLoginBody = `{
  "login": "ivanov",
  "password": "secret"
}`
	DocsExampleSignupConfigBody = `{
  "enableSignup": true,
  "defaultUserRole": "pending",
  "showAdminDetails": true
}`
	DocsExampleLdapToggleBody = `{
  "enableLdap": true
}`
	DocsExampleLdapServerBody = `{
  "label": "Active Directory",
  "host": "dc01.corp.local",
  "port": 636,
  "attributeForMail": "mail",
  "attributeForUsername": "sAMAccountName",
  "appDn": "CN=svc-docfinder,OU=Service,DC=corp,DC=local",
  "appDnPassword": "",
  "searchBase": "OU=Users,DC=corp,DC=local",
  "searchFilters": "(objectClass=user)",
  "useTls": true,
  "certificatePath": "",
  "validateCert": true,
  "ciphers": "ALL",
  "enableGroupManagement": false,
  "groupAttribute": "memberOf",
  "enableKerberos": false,
  "keytabPath": "/etc/docfinder/http.keytab",
  "servicePrincipal": "HTTP/docs.corp.local@CORP.LOCAL"
}`
	DocsExampleMessageBody = `{
  "text": "Как зарегистрировать входящий документ?",
  "processId": ""
}`
	DocsExampleRenameSessionBody = `{
  "title": "Регистрация входящих"
}`
	DocsExampleCreateProcessBody = `{
  "title": "Кадры",
  "selectPrompt": "",
  "answerPrompt": "",
  "clarifyPrompt": ""
}`
	DocsExampleCreateUserBody = `{
  "login": "clerk@corp.local",
  "password": "clerk",
  "displayName": "Делопроизводитель",
  "role": "user"
}`
	DocsExamplePatchUserBody = `{
  "displayName": "Сотрудник архива",
  "role": "user"
}`
	DocsExampleCreateRoleBody = `{
  "title": "Делопроизводитель",
  "description": "Общие документы подразделения",
  "ldapGroup": "Делопроизводители",
  "permissions": ["chat.use", "documents.global.manage"]
}`
	DocsExamplePatchRoleBody = `{
  "title": "Делопроизводитель",
  "description": "Общие документы и журнал",
  "ldapGroup": "Делопроизводители",
  "permissions": ["chat.use", "documents.global.manage", "audit.view"]
}`

	PhraseAccessDenied = "нет доступа"
	PhraseNotFound     = "не найдено"
	PhraseClarify      = "уточни"
	PhraseTemplate     = "шаблон"
	PhrasePrepare      = "как подготовить"

	BlankSkipNo   = "нет"
	BlankSkipPass = "пропустить"
	BlankSkipDash = "-"

	StatusAccepted = "accepted"

	SeedAdminLogin       = "admin"
	SeedAdminPassword    = "admin"
	SeedAdminID          = "user-admin"
	SeedAdminDisplayName = "Администратор"
	SeedAdminAccessLevel = 2

	SeedEmployeeLogin       = "user"
	SeedEmployeePassword    = "user"
	SeedEmployeeID          = "user-1"
	SeedEmployeeDisplayName = "Сотрудник"
	SeedEmployeeAccessLevel = 1

	SeedInstructionID        = "doc-1"
	SeedInstructionTitle     = "Инструкция по документообороту"
	SeedInstructionFileName  = "instruction.txt"
	SeedInstructionBody      = "Инструкция по документообороту. Пункт 3.2. Входящий документ регистрируют в день поступления."
	SeedRestrictedID         = "doc-2"
	SeedRestrictedTitle      = "Закрытый регламент"
	SeedRestrictedFileName   = "restricted.txt"
	SeedRestrictedBody       = "Закрытый регламент. Доступ ограничен."
	SeedDocumentAccessOpen   = 1
	SeedDocumentAccessClosed = 2

	EditionDefault       = "1.0"
	DefaultDocumentTitle = "Документ"
	PlainTextContentType = "text/plain; charset=utf-8"
	DocxContentType      = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

	ClarifyText      = "Уточните, о каком типе документа идет речь."
	FallbackText     = "В базе знаний нет сведений для ответа на этот вопрос."
	AccessDeniedText = "Недостаточно прав для просмотра документов по этому запросу."

	TemplateReadyText = "Черновик собран."

	PrepareParagraphComposition = "8.1"
	PrepareParagraphList        = "8.2"

	LiveSelectPrompt   = "По списку фрагментов верни только номера тех, которые нужны для ответа на вопрос. Номера через запятую. Если ни один фрагмент не подходит, верни нет. Не пиши сам ответ и не добавляй фактов."
	LiveAnswerPrompt   = "Отвечай только по переданным фрагментам инструкций. Не добавляй факты, которых в них нет. Сложные формулировки и термины объясни простыми словами. Не копируй текст пункта дословно. Коротко, по-русски."
	LiveClarifyPrompt  = "Вопрос подходит к разным документам. Задай один короткий уточняющий вопрос, о каком документе речь. Не отвечай по существу и не добавляй фактов."
	LiveQuestionLabel  = "Вопрос"
	LiveFragmentsLabel = "Фрагменты"
	LiveDocumentLabel  = "Документ"
	LiveEditionLabel   = "Редакция"
	LiveSectionLabel   = "Раздел"
	LiveParagraphLabel = "Пункт"
	LiveAppendixLabel  = "Приложение"

	// Добавляется к промпту ответа любой темы: промпт темы хранится в базе и мог быть переписан в админке,
	// а отказ меткой - часть контракта ядра. Не слово «нет»: оно же честный ответ на «можно ли»
	LiveAnswerRefusalRule  = "Если в переданных фрагментах нет ответа на вопрос, верни только метку НЕТ_ОТВЕТА. На вопрос «можно ли» отвечай по сути, словами да или нет с пояснением."
	LiveAnswerNoAnswerMark = "НЕТ_ОТВЕТА"

	// Ответ модели с числом, которого нет во фрагментах, заменяется ближайшим пунктом дословно
	LiveUnsupportedIntro = "Точного ответа сформулировать не удалось. Ближайший по смыслу пункт:"

	// «Карта» выбранного процесса: добавляется к подсказкам модели, чтобы она знала, что есть в теме
	LiveProcessLabel    = "Тема поиска"
	LiveProcessContents = "Документы темы"
	LiveDraftPrompt     = "Перепиши переданную часть черновика по просьбе сотрудника. Опирайся только на переданные фрагменты инструкции и не добавляй фактов, которых в них нет. Если фрагментов нет, правь формулировку, но не выдумывай пункты. Верни только JSON {\"draftText\":\"новый текст части\",\"changes\":[{\"text\":\"что изменено\",\"paragraph\":\"номер пункта\"}]}. paragraph указывай только если пункт есть во фрагментах. changes может быть пустым массивом."
	LiveDraftPartLabel  = "Часть черновика"

	DraftRevisionKept = "Черновик обновлен."

	DraftRecommendation   = "В проекте нет отметки о приложении."
	DraftSignatureMissing = "В проекте нет подписи."

	EvalCoreUnavailable = "Живое ядро недоступно."
	EvalLoginFailed     = "Не удалось войти в живое ядро."
	EvalRequestFailed   = "Запрос к живому ядру не выполнен."
	EvalPointsLine      = "верных пунктов: %d из %d\n"
	EvalFallbackLine    = "fallback: %d из %d\n"
	EvalFactsLine       = "ответов с нужными фактами: %d из %d\n"
	EvalRefusalLine     = "верных отказов: %d из %d\n"
	EvalFalseAnswerLine = "ответов вместо отказа: %d\n"
	EvalMissedLine      = "отказов на вопросы из базы: %d из %d\n"
	EvalUnsupportedLine = "ответов с числами не из источников: %d из %d\n"
	EvalQuoteLine       = "ответов заменено цитатой пункта: %d\n"
	EvalHonestLine      = "ответов без выдумки: %d из %d (%d%%)\n"
	EvalQuestionLine    = "%s: %s%s\n"
	EvalMarkPoint       = " [не тот пункт]"
	EvalMarkFacts       = " [нет нужного факта]"
	EvalMarkFalseAnswer = " [ответ вместо отказа]"
	EvalMarkMissed      = " [отказ на вопрос из базы]"
	EvalMarkQuote       = " [цитата пункта]"
	EvalMarkUnsupported = " [числа не из источников: %s]"
)

type PermissionInfo struct {
	Permission  string
	Title       string
	Description string
}

func Permission_Catalog() []PermissionInfo {
	return []PermissionInfo{
		{PermissionChatUse, "Вопросы помощнику", "Задавать вопросы помощнику"},
		{PermissionPersonalUpload, "Свои документы", "Загружать личные документы"},
		{PermissionGlobalManage, "Общие документы", "Загружать, заменять и удалять общие документы"},
		{PermissionPersonalViewAll, "Документы сотрудников", "Видеть и удалять личные документы сотрудников"},
		{PermissionDraftsCheck, "Проверка черновика", "Сверять готовый документ с инструкцией"},
		{PermissionDraftsAssist, "Помощь с черновиком", "Просить помощника править текст черновика"},
		{PermissionTemplatesUse, "Бланки", "Заполнять модульные бланки"},
		{PermissionUsersManage, "Пользователи", "Создавать учетные записи и назначать роли"},
		{PermissionRolesManage, "Роли", "Создавать, менять и удалять роли"},
		{PermissionAuditView, "Журналы", "Смотреть журнал вопросов и журнал изменений прав"},
		{PermissionAIManage, "Модели ИИ", "Подключение к Ollama, выбор и скачивание моделей, параметры ответа"},
		{PermissionOrganizationManage, "Реквизиты организации", "Генеральный директор, должностные лица, герб и штамп для документов"},
		{PermissionSourcesManage, "Источники документов", "Автозагрузка из папки и лицензионного API, включение по расписанию"},
	}
}

// Настройка локальной модели из админ-панели
const (
	// Пробный текст для проверки размера вектора новой модели эмбеддингов
	AIEmbedProbe = "Проверка модели эмбеддингов"
	// Вопрос для кнопки «Проверить» у модели
	AITestPrompt = "Ответь одной короткой фразой по-русски: ты на связи?"
)

// Автозагрузка документов: виды источников и демо-фрагмент для проверки цикла
const (
	SourceKindFolderTitle       = "Каталог на диске"
	SourceKindFolderLead        = "Папка с .txt, .pdf или .docx. Так же забирается лицензионная выгрузка Консультант Плюс."
	SourceKindDemoTitle         = "Демо"
	SourceKindDemoLead          = "Один встроенный фрагмент. Нужен, чтобы проверить синхронизацию без сети."
	SourceKindConsultantTitle   = "Консультант Плюс"
	SourceKindConsultantLead    = "Лицензионный API организации. Адрес и ключ выдаёт поставщик. Сайт консультанта не разбираем."
	SourceDemoTitle             = "Демо-фрагмент источника"
	SourceDemoBody              = "Пункт 1.1. Входящий документ регистрируют в день поступления.\n"
	SourceStatusIdle            = "idle"
	SourceStatusRunning         = "running"
	SourceStatusOK              = "ok"
	SourceStatusError           = "error"
	DocsHintSources             = "Список источников автозагрузки. Нужен флаг sources.manage. Секрет лицензии в ответе не приходит, только licenseKeySet."
	DocsHintSourceKinds         = "Виды коннекторов и поля настроек. folder, demo, consultantplus."
	DocsHintCreateSource        = "Новый источник. enabled по умолчанию false. kind: folder, demo или consultantplus. Включить consultantplus без baseUrl и licenseKey нельзя."
	DocsHintPatchSource         = "Имя, интервал, процесс, роли, config, enabled. Пустой licenseKey оставляет прежний ключ."
	DocsHintDeleteSource        = "Удаляет источник. Загруженные документы остаются в базе, связь с источником снимается. Ответ 204."
	DocsHintVerifySource        = "Проверка доступа: папка читается, лицензионный API отвечает на /v1/status."
	DocsHintSyncSource          = "Внеочередной прогон. Выключенный источник цикл сам не трогает, эта кнопка работает."
	DocsHintSourceRuns          = "Статус последнего прогона: lastStatus, lastError, lastSyncAt."
	DocsExampleCreateSourceBody = `{
  "kind": "demo",
  "title": "Демо",
  "intervalMinutes": 60
}`
)
