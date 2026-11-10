package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnswerSendsContractAndToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/answer" || request.Method != http.MethodPost {
			t.Fatalf("request %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer engine-secret" {
			t.Fatalf("authorization %q", request.Header.Get("Authorization"))
		}
		var body struct {
			Question string        `json:"question"`
			History  []Turn        `json:"history"`
			Settings ModelSettings `json:"settings"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Question != "Какой срок?" || len(body.History) != 1 || body.History[0].Text != "Про аренду" {
			t.Fatalf("body %+v", body)
		}
		if body.Settings.BaseURL != "http://model/v1" || body.Settings.LLMModel != "legal-model" {
			t.Fatalf("settings %+v", body.Settings)
		}
		_ = json.NewEncoder(response).Encode(Answer{
			Kind:      "answer",
			Text:      "30 дней",
			Citations: []Citation{{DocID: "contract", Document: "Договор", Path: "4.2"}},
		})
	}))
	defer server.Close()

	client := New_Client(server.URL, "engine-secret")
	answer, err := client.Answer(context.Background(), "Какой срок?", []Turn{{Role: "user", Text: "Про аренду"}}, ModelSettings{BaseURL: "http://model/v1", LLMModel: "legal-model"})
	if err != nil || answer.Text != "30 дней" || len(answer.Citations) != 1 {
		t.Fatalf("answer %+v error %v", answer, err)
	}
}

func TestStructureAndUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/documents/missing/structure" {
			response.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"doc_id": "doc-a", "chunks": []any{}})
	}))
	defer server.Close()

	client := New_Client(server.URL, "")
	if raw, err := client.Structure(context.Background(), "doc-a"); err != nil || len(raw) == 0 {
		t.Fatalf("structure %s error %v", raw, err)
	}
	if _, err := client.Structure(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing error %v", err)
	}

	emptyServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(response).Encode(Answer{Kind: "answer"})
	}))
	defer emptyServer.Close()
	if _, err := New_Client(emptyServer.URL, "").Answer(context.Background(), "q", nil, ModelSettings{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("empty answer error %v", err)
	}
}
