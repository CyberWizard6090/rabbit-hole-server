package service

import (
	"encoding/json"

	"rabbit-hole-server/internal/domain"
)

type FormPresetOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type FormPresetQuestion struct {
	Type        domain.QuestionType `json:"type"`
	Title       string              `json:"title"`
	Description string              `json:"description,omitempty"`
	Required    bool                `json:"required"`
	Settings    json.RawMessage     `json:"settings,omitempty"`
	Options     []FormPresetOption  `json:"options,omitempty"`
}

type FormPreset struct {
	Key         string               `json:"key"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Questions   []FormPresetQuestion `json:"questions"`
}

type presetText struct{ en, ru string }

type presetQuestion struct {
	kind     domain.QuestionType
	title    presetText
	help     presetText
	required bool
	settings string
	options  []presetOption
}

type presetOption struct {
	value string
	label presetText
}

type presetDefinition struct {
	key       string
	name      presetText
	help      presetText
	questions []presetQuestion
}

func FormPresets(locale string) []FormPreset {
	pick := func(text presetText) string {
		if locale == localeRussian {
			return text.ru
		}
		return text.en
	}
	presets := make([]FormPreset, 0, len(formPresetDefinitions))
	for _, definition := range formPresetDefinitions {
		preset := FormPreset{
			Key: definition.key, Name: pick(definition.name), Description: pick(definition.help),
			Questions: make([]FormPresetQuestion, len(definition.questions)),
		}
		for i, question := range definition.questions {
			item := FormPresetQuestion{
				Type: question.kind, Title: pick(question.title), Description: pick(question.help),
				Required: question.required,
			}
			if question.settings != "" {
				item.Settings = json.RawMessage(question.settings)
			}
			for _, option := range question.options {
				item.Options = append(item.Options, FormPresetOption{Label: pick(option.label), Value: option.value})
			}
			preset.Questions[i] = item
		}
		presets = append(presets, preset)
	}
	return presets
}

func choiceOptions(values ...string) []presetOption {
	options := make([]presetOption, 0, len(values)/3)
	for i := 0; i+2 < len(values); i += 3 {
		options = append(options, presetOption{
			value: values[i], label: presetText{en: values[i+1], ru: values[i+2]},
		})
	}
	return options
}

var formPresetDefinitions = []presetDefinition{
	{
		key:  "feedback",
		name: presetText{"Feedback", "Обратная связь"},
		help: presetText{"Collect opinions and ideas from customers or colleagues.", "Соберите отзывы и идеи клиентов или коллег."},
		questions: []presetQuestion{
			{kind: domain.QuestionRating, title: presetText{"How would you rate us?", "Как вы нас оцениваете?"},
				required: true, settings: `{"min":1,"max":5}`},
			{kind: domain.QuestionSingleChoice, title: presetText{"What is your feedback about?", "О чём ваш отзыв?"},
				required: true, options: choiceOptions(
					"product", "Product", "Продукт", "support", "Support", "Поддержка",
					"pricing", "Pricing", "Цены", "other", "Other", "Другое")},
			{kind: domain.QuestionLongText, title: presetText{"Tell us more", "Расскажите подробнее"}, required: true},
			{kind: domain.QuestionEmail, title: presetText{"Your email (optional)", "Ваш email (необязательно)"}},
		},
	},
	{
		key:  "project_request",
		name: presetText{"Project request", "Заявка на проект"},
		help: presetText{"Let teammates propose new projects with a scope and a deadline.", "Позвольте коллегам предлагать проекты с описанием и сроками."},
		questions: []presetQuestion{
			{kind: domain.QuestionShortText, title: presetText{"Project name", "Название проекта"}, required: true,
				settings: `{"max_length":200}`},
			{kind: domain.QuestionLongText, title: presetText{"Goals and scope", "Цели и объём работ"}, required: true},
			{kind: domain.QuestionDropdown, title: presetText{"Project type", "Тип проекта"}, required: true,
				options: choiceOptions(
					"internal", "Internal", "Внутренний", "client", "For a client", "Для клиента",
					"research", "Research", "Исследование")},
			{kind: domain.QuestionDate, title: presetText{"Desired deadline", "Желаемый срок"}},
			{kind: domain.QuestionNumber, title: presetText{"Estimated budget", "Ориентировочный бюджет"},
				settings: `{"min":0}`},
			{kind: domain.QuestionEmail, title: presetText{"Contact email", "Контактный email"}, required: true},
		},
	},
	{
		key:  "order",
		name: presetText{"Order", "Заказ"},
		help: presetText{"Take simple orders with contact details and delivery information.", "Принимайте простые заказы с контактами и данными доставки."},
		questions: []presetQuestion{
			{kind: domain.QuestionShortText, title: presetText{"Full name", "Имя и фамилия"}, required: true},
			{kind: domain.QuestionPhone, title: presetText{"Phone", "Телефон"}, required: true},
			{kind: domain.QuestionEmail, title: presetText{"Email", "Email"}},
			{kind: domain.QuestionDropdown, title: presetText{"Product", "Товар"}, required: true,
				options: choiceOptions(
					"basic", "Basic", "Базовый", "standard", "Standard", "Стандартный",
					"premium", "Premium", "Премиум")},
			{kind: domain.QuestionNumber, title: presetText{"Quantity", "Количество"}, required: true,
				settings: `{"min":1,"max":1000}`},
			{kind: domain.QuestionLongText, title: presetText{"Delivery address", "Адрес доставки"}, required: true},
			{kind: domain.QuestionDate, title: presetText{"Delivery date", "Дата доставки"}},
		},
	},
	{
		key:  "application",
		name: presetText{"Application", "Анкета кандидата"},
		help: presetText{"Collect applications for a position or a program.", "Собирайте анкеты на вакансию или программу."},
		questions: []presetQuestion{
			{kind: domain.QuestionShortText, title: presetText{"Full name", "Имя и фамилия"}, required: true},
			{kind: domain.QuestionEmail, title: presetText{"Email", "Email"}, required: true},
			{kind: domain.QuestionPhone, title: presetText{"Phone", "Телефон"}},
			{kind: domain.QuestionSingleChoice, title: presetText{"Years of experience", "Опыт работы"}, required: true,
				options: choiceOptions(
					"0-1", "Up to 1 year", "До 1 года", "1-3", "1 to 3 years", "От 1 до 3 лет",
					"3-5", "3 to 5 years", "От 3 до 5 лет", "5+", "More than 5 years", "Более 5 лет")},
			{kind: domain.QuestionURL, title: presetText{"Portfolio or profile link", "Ссылка на портфолио или профиль"}},
			{kind: domain.QuestionLongText, title: presetText{"Why do you want to join?", "Почему вы хотите к нам?"},
				required: true},
		},
	},
	{
		key:  "it_request",
		name: presetText{"IT request", "Заявка в ИТ"},
		help: presetText{"Let employees report IT problems and request access or equipment.", "Сотрудники сообщают о проблемах и запрашивают доступы или оборудование."},
		questions: []presetQuestion{
			{kind: domain.QuestionShortText, title: presetText{"Summary", "Краткое описание"}, required: true,
				settings: `{"max_length":200}`},
			{kind: domain.QuestionDropdown, title: presetText{"Category", "Категория"}, required: true,
				options: choiceOptions(
					"access", "Access", "Доступ", "hardware", "Hardware", "Оборудование",
					"software", "Software", "Программы", "network", "Network", "Сеть",
					"other", "Other", "Другое")},
			{kind: domain.QuestionSingleChoice, title: presetText{"Urgency", "Срочность"}, required: true,
				options: choiceOptions(
					"low", "Low", "Низкая", "normal", "Normal", "Обычная",
					"high", "High", "Высокая", "urgent", "Urgent", "Срочно")},
			{kind: domain.QuestionLongText, title: presetText{"Details", "Подробности"}, required: true},
			{kind: domain.QuestionPhone, title: presetText{"Phone for a call back", "Телефон для связи"}},
		},
	},
}
