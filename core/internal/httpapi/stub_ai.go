package httpapi

import (
	"context"
	"errors"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/livestore"
	"docfinder/core/internal/ollama"
	"docfinder/core/internal/texts"
)

// В заглушке вкладка «Модели ИИ» работает с настоящей Ollama по OLLAMA_URL: модели, скачивание и проверка
// ответа - реальные. Настройки живут в памяти до перезапуска, а переиндексации нет: у заглушки нет векторов

var errStubWithoutIndex = errors.New("stub without index")

type stubReindexer struct{}

func (stubReindexer) Read_ReindexStatus(ctx context.Context, token string) (livestore.ReindexStatus, error) {
	return livestore.ReindexStatus{}, errStubWithoutIndex
}

func (stubReindexer) Start_Reindex(ctx context.Context, token string) (livestore.ReindexStatus, error) {
	return livestore.ReindexStatus{}, errStubWithoutIndex
}

func (stubStore *stub) create_AIAdmin(client *ollama.Client, dimensions int) *aiAdmin {
	return &aiAdmin{
		backend:    client,
		dimensions: dimensions,
		authorize:  stubStore.authorize_AIManager,
		persist: func(ctx context.Context, token string, settings ollama.Settings) error {
			return nil
		},
		reindexer: stubReindexer{},
	}
}

func (stubStore *stub) authorize_AIManager(ctx context.Context, token string) error {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	record := stubStore.usersByID[stubStore.tokens[token]]
	if record == nil {
		return livestore.ErrUnauthorized
	}
	if record.pending {
		return livestore.ErrAccountPending
	}
	if !auth.Has_Permission(stubStore.permissions_Of(record), texts.PermissionAIManage) {
		return livestore.ErrAccessDenied
	}
	return nil
}
