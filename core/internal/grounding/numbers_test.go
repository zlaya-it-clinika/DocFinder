package grounding

import (
	"strings"
	"testing"
)

func TestFind_UnsupportedNumbersKeepsSourceNumbers(t *testing.T) {
	sources := []string{
		"Когда документ поступил вечером?",
		"Инструкция по ДОУ 5.6 Входящий документ регистрируется не позднее следующего рабочего дня. Срок 3 (трех) рабочих дней.",
		"Подписание производится не более чем за три рабочих дня, приказ от 15 октября 2026 г. № ОД-184.",
	}
	answer := "По п. 5.6 регистрация в течение 3 рабочих дней, подписание за 3 дня, приказ № ОД-184 от 15 октября 2026 г."
	if unsupported := Find_UnsupportedNumbers(answer, sources); len(unsupported) != 0 {
		t.Fatalf("unsupported %v", unsupported)
	}
}

func TestFind_UnsupportedNumbersFindsInvented(t *testing.T) {
	sources := []string{"Срок исполнения обращений граждан 30 календарных дней."}
	unsupported := Find_UnsupportedNumbers("Обращения исполняются за 14 дней, по п. 6.6, как и 30-дневные.", sources)
	if strings.Join(unsupported, " ") != "14 6.6" {
		t.Fatalf("unsupported %v", unsupported)
	}
}

func TestFind_UnsupportedNumbersSkipsListMarkers(t *testing.T) {
	sources := []string{"Визирует ответственный исполнитель, затем руководитель."}
	answer := "1. Ответственный исполнитель.\n2) Руководитель.\n  3. Готово."
	if unsupported := Find_UnsupportedNumbers(answer, sources); len(unsupported) != 0 {
		t.Fatalf("list markers counted %v", unsupported)
	}
	if unsupported := Find_UnsupportedNumbers("Срок 5 дней.", sources); len(unsupported) != 1 || unsupported[0] != "5" {
		t.Fatalf("number in text skipped %v", unsupported)
	}
}

func TestFind_UnsupportedNumbersNormalizesTokens(t *testing.T) {
	sources := []string{"Пункт 8.12.4. Не более трёх адресатов, срок 0,5 часа."}
	if unsupported := Find_UnsupportedNumbers("См. 8.12.4, не более 3 адресатов, 0.5 часа.", sources); len(unsupported) != 0 {
		t.Fatalf("unsupported %v", unsupported)
	}
}
