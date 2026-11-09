package domain

const (
	ListTemplateSimple      = "simple"
	ListTemplateKanban      = "kanban"
	ListTemplateDevelopment = "development"
	ListTemplateBugs        = "bugs"

	DefaultListTemplate = ListTemplateSimple
	listLocaleRussian   = "ru"
)

type localized map[string]string

func (l localized) in(locale string) string {
	if value, ok := l[locale]; ok {
		return value
	}
	return l["en"]
}

type templateStatus struct {
	names localized
	color string
	kind  StatusType
}

type ListTemplate struct {
	Key          string
	names        localized
	descriptions localized
	statuses     []templateStatus
}

type ListTemplateStatusView struct {
	Name  string     `json:"name"`
	Color string     `json:"color"`
	Type  StatusType `json:"type"`
}

type ListTemplateView struct {
	Key         string                   `json:"key"`
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Statuses    []ListTemplateStatusView `json:"statuses"`
}

const (
	colorGray   = "#94A3B8"
	colorBlue   = "#3B82F6"
	colorViolet = "#8B5CF6"
	colorAmber  = "#F59E0B"
	colorGreen  = "#22C55E"
	colorRed    = "#EF4444"
)

func status(en, ru, color string, kind StatusType) templateStatus {
	return templateStatus{names: localized{"en": en, "ru": ru}, color: color, kind: kind}
}

var listTemplates = []ListTemplate{
	{
		Key:          ListTemplateSimple,
		names:        localized{"en": "Simple", "ru": "Простой"},
		descriptions: localized{"en": "To do, in progress, done", "ru": "К выполнению, в работе, готово"},
		statuses: []templateStatus{
			status("To do", "К выполнению", colorGray, StatusTodo),
			status("In progress", "В работе", colorBlue, StatusInProgress),
			status("Done", "Готово", colorGreen, StatusDone),
		},
	},
	{
		Key:          ListTemplateKanban,
		names:        localized{"en": "Kanban", "ru": "Канбан"},
		descriptions: localized{"en": "Backlog, to do, in progress, review, done", "ru": "Бэклог, к выполнению, в работе, на проверке, готово"},
		statuses: []templateStatus{
			status("Backlog", "Бэклог", colorGray, StatusTodo),
			status("To do", "К выполнению", colorBlue, StatusTodo),
			status("In progress", "В работе", colorAmber, StatusInProgress),
			status("In review", "На проверке", colorViolet, StatusInProgress),
			status("Done", "Готово", colorGreen, StatusDone),
		},
	},
	{
		Key:          ListTemplateDevelopment,
		names:        localized{"en": "Development", "ru": "Разработка"},
		descriptions: localized{"en": "Backlog, analysis, development, code review, testing, done", "ru": "Бэклог, анализ, разработка, код-ревью, тестирование, готово"},
		statuses: []templateStatus{
			status("Backlog", "Бэклог", colorGray, StatusTodo),
			status("Analysis", "Анализ", colorBlue, StatusTodo),
			status("In development", "В разработке", colorAmber, StatusInProgress),
			status("Code review", "Код-ревью", colorViolet, StatusInProgress),
			status("Testing", "Тестирование", colorAmber, StatusInProgress),
			status("Done", "Готово", colorGreen, StatusDone),
		},
	},
	{
		Key:          ListTemplateBugs,
		names:        localized{"en": "Bugs", "ru": "Баги"},
		descriptions: localized{"en": "New, confirmed, fixing, verification, closed, rejected", "ru": "Новый, подтверждён, исправляется, проверка, закрыт, отклонён"},
		statuses: []templateStatus{
			status("New", "Новый", colorGray, StatusTodo),
			status("Confirmed", "Подтверждён", colorBlue, StatusTodo),
			status("Fixing", "Исправляется", colorAmber, StatusInProgress),
			status("Verification", "Проверка", colorViolet, StatusInProgress),
			status("Closed", "Закрыт", colorGreen, StatusDone),
			status("Rejected", "Отклонён", colorRed, StatusDone),
		},
	},
}

func ListTemplateByKey(key string) (ListTemplate, bool) {
	if key == "" {
		key = DefaultListTemplate
	}
	for _, t := range listTemplates {
		if t.Key == key {
			return t, true
		}
	}
	return ListTemplate{}, false
}

func ListTemplates(locale string) []ListTemplateView {
	out := make([]ListTemplateView, len(listTemplates))
	for i, t := range listTemplates {
		out[i] = t.View(locale)
	}
	return out
}

func (t ListTemplate) Statuses(locale string) []TaskStatus {
	out := make([]TaskStatus, len(t.statuses))
	for i, s := range t.statuses {
		out[i] = TaskStatus{Name: s.names.in(locale), Color: s.color, Position: i + 1, Type: s.kind}
	}
	return out
}

func (t ListTemplate) View(locale string) ListTemplateView {
	statuses := t.Statuses(locale)
	view := ListTemplateView{
		Key: t.Key, Name: t.names.in(locale), Description: t.descriptions.in(locale),
		Statuses: make([]ListTemplateStatusView, len(statuses)),
	}
	for i, s := range statuses {
		view.Statuses[i] = ListTemplateStatusView{Name: s.Name, Color: s.Color, Type: s.Type}
	}
	return view
}

func DefaultListName(locale string) string {
	if locale == listLocaleRussian {
		return "Основной список"
	}
	return "Main list"
}
