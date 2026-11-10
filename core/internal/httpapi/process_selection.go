package httpapi

import (
	"strings"

	"docfinder/core/internal/texts"
)

// Выбор категорий документов в чате: один процесс, несколько («Документооборот» и «Финансы») или все.
// Внутри ядра и в сессии выбор - коды процессов через запятую, пустая строка - все процессы.
// Один код без запятой - прежний формат, поэтому сохраненные сессии читаются как раньше

func split_ProcessSelection(raw string) []string {
	ids := []string{}
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, texts.ProcessSelectionSeparator) {
		id := strings.TrimSpace(part)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

func join_ProcessSelection(ids []string) string {
	return strings.Join(split_ProcessSelection(strings.Join(ids, texts.ProcessSelectionSeparator)), texts.ProcessSelectionSeparator)
}

// processIds в запросе важнее processId. nil - выбор, сохраненный в сессии
func read_RequestedProcesses(single *string, list *[]string) *string {
	if list != nil {
		joined := join_ProcessSelection(*list)
		return &joined
	}
	if single != nil {
		joined := join_ProcessSelection([]string{*single})
		return &joined
	}
	return nil
}

func check_ProcessSelected(selection, processID string) bool {
	if selection == "" {
		return true
	}
	for _, id := range split_ProcessSelection(selection) {
		if id == processID {
			return true
		}
	}
	return false
}
