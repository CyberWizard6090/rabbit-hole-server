package domain

import "testing"

func TestListTemplatesAreCompleteInEveryLocale(t *testing.T) {
	for _, locale := range []string{"en", "ru", "de"} {
		for _, view := range ListTemplates(locale) {
			if view.Key == "" || view.Name == "" || view.Description == "" || len(view.Statuses) < 3 {
				t.Fatalf("%s/%s: %+v", locale, view.Key, view)
			}
			sawDone := false
			for _, s := range view.Statuses {
				if s.Name == "" || len(s.Color) != 7 || s.Type == StatusTypeUndefined {
					t.Fatalf("%s/%s: bad status %+v", locale, view.Key, s)
				}
				sawDone = sawDone || s.Type == StatusDone
			}
			if !sawDone {
				t.Fatalf("%s/%s has no closing status", locale, view.Key)
			}
		}
	}
}

func TestListTemplateRussianDiffersFromEnglishAndUnknownLocaleFallsBack(t *testing.T) {
	en, _ := ListTemplateByKey(ListTemplateKanban)
	if en.Statuses("ru")[0].Name == en.Statuses("en")[0].Name {
		t.Fatal("the Russian names must differ from the English ones")
	}
	if en.Statuses("de")[0].Name != en.Statuses("en")[0].Name {
		t.Fatal("an unknown locale falls back to English")
	}
	if _, ok := ListTemplateByKey(""); !ok {
		t.Fatal("an empty key means the default template")
	}
	if _, ok := ListTemplateByKey("nope"); ok {
		t.Fatal("an unknown key must be refused")
	}
	if DefaultListName("ru") != "Основной список" || DefaultListName("en") != "Main list" {
		t.Fatal("default list name is localized")
	}
}
