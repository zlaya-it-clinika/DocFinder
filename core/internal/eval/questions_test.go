package eval

import "testing"

func TestLoad_QuestionsAndMatch(t *testing.T) {
	questions, err := Load_Questions()
	if err != nil {
		t.Fatal(err)
	}
	if len(questions) < 50 || questions[0].Paragraph != "5.1" || !questions[2].Fallback {
		t.Fatalf("questions %d %+v", len(questions), questions[:3])
	}
	// Вопрос либо ждет отказа, либо указывает пункт, но не то и другое сразу
	for _, question := range questions {
		if question.Question == "" || question.Fallback == (question.Paragraph != "") || (question.Fallback && len(question.Facts) > 0) {
			t.Fatalf("question %+v", question)
		}
	}
	if !Paragraph_Matches("5.1", "5.1") || !Paragraph_Matches("5.1.1", "5.1") || Paragraph_Matches("3.1.1", "5.1") || Paragraph_Matches("", "5.1") {
		t.Fatal("paragraph match")
	}
}

func TestFacts_Match(t *testing.T) {
	answer := "Исполнить в течение 3 рабочих дней, срок считается с ДАТЫ регистрации, трёх дней хватит."
	if !Facts_Match(answer, []string{"3 |трех", "регистрации"}) || !Facts_Match(answer, []string{"трех"}) || !Facts_Match(answer, nil) {
		t.Fatal("facts not found")
	}
	if Facts_Match(answer, []string{"регистрации", "30|тридцат"}) || Facts_Match("Срок 13 дней.", []string{"3 |трех"}) {
		t.Fatal("missing fact matched")
	}
}

func TestCitation_MatchesDocument(t *testing.T) {
	matrix := Question{Paragraph: "1", Document: "Матрица"}
	if !Citation_Matches(matrix, "Матрица обязательного визирования", "1") || Citation_Matches(matrix, "Инструкция по ДОУ", "1.1") || Citation_Matches(matrix, "Матрица обязательного визирования", "2") {
		t.Fatal("document match")
	}
	if !Citation_Matches(Question{Paragraph: "5.1"}, "Инструкция по ДОУ", "5.1.2") {
		t.Fatal("any document")
	}
}
