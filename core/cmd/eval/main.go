package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"docfinder/core/internal/eval"
	"docfinder/core/internal/grounding"
	"docfinder/core/internal/texts"
)

type assistantLine struct {
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	Citations []struct {
		Document  string `json:"document"`
		Edition   string `json:"edition"`
		Section   string `json:"section"`
		Paragraph string `json:"paragraph"`
		Appendix  string `json:"appendix"`
		Quote     string `json:"quote"`
	} `json:"citations"`
}

// Итоги прогона. «Без выдумки» - верный отказ, отказ вместо ответа или ответ, все числа которого есть в источниках
type evalTotals struct {
	answered         int
	pointQuestions   int
	pointHits        int
	factQuestions    int
	factHits         int
	fallbackCount    int
	refusalQuestions int
	refusalHits      int
	falseAnswers     int
	baseQuestions    int
	missedAnswers    int
	modelAnswers     int
	unsupported      int
	quoted           int
	honest           int
}

func main() {
	questions, err := eval.Load_Questions()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	apiURL := strings.TrimRight(os.Getenv("EVAL_API_URL"), "/")
	if apiURL == "" {
		apiURL = "http://127.0.0.1:8080"
	}
	// В live учётные записи входят по почте, поэтому сотрудник для прогона задается через окружение.
	// Без переменных - сотрудник заглушки
	login := os.Getenv("EVAL_LOGIN")
	password := os.Getenv("EVAL_PASSWORD")
	if login == "" {
		login = texts.SeedEmployeeLogin
		password = texts.SeedEmployeePassword
	}
	client := &http.Client{Timeout: 3 * time.Minute}
	token, loginError := login_Employee(client, apiURL, login, password)
	if loginError != nil {
		fmt.Println(loginError.Error())
		return
	}
	totals := evalTotals{}
	for _, question := range questions {
		if question.Paragraph != "" {
			totals.pointQuestions++
		}
		if len(question.Facts) > 0 {
			totals.factQuestions++
		}
		if question.Fallback {
			totals.refusalQuestions++
		} else {
			totals.baseQuestions++
		}
		reply, askError := ask_Question(client, apiURL, token, question.Question)
		// Прокси портов Docker Desktop изредка рвет соединение, а POST сам не повторяется: одна повторная попытка
		if askError != nil && askError.Error() == texts.EvalCoreUnavailable {
			reply, askError = ask_Question(client, apiURL, token, question.Question)
		}
		if askError != nil {
			fmt.Printf("%s: %s\n", question.Question, askError.Error())
			continue
		}
		fmt.Printf(texts.EvalQuestionLine, question.Question, reply.Kind, totals.add_Reply(question, reply))
	}
	totals.print_Summary()
}

// Учитывает один ответ и возвращает пометки для строки вопроса
func (totals *evalTotals) add_Reply(question eval.Question, reply assistantLine) string {
	totals.answered++
	marks := ""
	isAnswer := reply.Kind == texts.KindAnswer
	if reply.Kind == texts.KindFallback {
		totals.fallbackCount++
	}
	if question.Fallback {
		if isAnswer {
			totals.falseAnswers++
			marks += texts.EvalMarkFalseAnswer
		} else {
			totals.refusalHits++
			totals.honest++
		}
		return marks
	}
	if !isAnswer {
		totals.missedAnswers++
		marks += texts.EvalMarkMissed
	}
	if question.Paragraph != "" {
		if reply_HasParagraph(reply, question) {
			totals.pointHits++
		} else {
			marks += texts.EvalMarkPoint
		}
	}
	if len(question.Facts) > 0 {
		if isAnswer && eval.Facts_Match(reply.Text, question.Facts) {
			totals.factHits++
		} else {
			marks += texts.EvalMarkFacts
		}
	}
	if !isAnswer {
		totals.honest++
		return marks
	}
	totals.modelAnswers++
	if strings.HasPrefix(reply.Text, texts.LiveUnsupportedIntro) {
		totals.quoted++
		marks += texts.EvalMarkQuote
	}
	unsupported := grounding.Find_UnsupportedNumbers(reply.Text, reply_Sources(question.Question, reply))
	if len(unsupported) > 0 {
		totals.unsupported++
		marks += fmt.Sprintf(texts.EvalMarkUnsupported, strings.Join(unsupported, ", "))
		return marks
	}
	totals.honest++
	return marks
}

func (totals evalTotals) print_Summary() {
	fmt.Printf(texts.EvalPointsLine, totals.pointHits, totals.pointQuestions)
	fmt.Printf(texts.EvalFactsLine, totals.factHits, totals.factQuestions)
	fmt.Printf(texts.EvalRefusalLine, totals.refusalHits, totals.refusalQuestions)
	fmt.Printf(texts.EvalFalseAnswerLine, totals.falseAnswers)
	fmt.Printf(texts.EvalMissedLine, totals.missedAnswers, totals.baseQuestions)
	fmt.Printf(texts.EvalUnsupportedLine, totals.unsupported, totals.modelAnswers)
	fmt.Printf(texts.EvalQuoteLine, totals.quoted)
	fmt.Printf(texts.EvalFallbackLine, totals.fallbackCount, totals.answered)
	percent := 0
	if totals.answered > 0 {
		percent = totals.honest * 100 / totals.answered
	}
	fmt.Printf(texts.EvalHonestLine, totals.honest, totals.answered, percent)
}

func reply_HasParagraph(reply assistantLine, question eval.Question) bool {
	for _, citation := range reply.Citations {
		if eval.Citation_Matches(question, citation.Document, citation.Paragraph) {
			return true
		}
	}
	return false
}

// Те же источники, что видит проверка в ядре: вопрос и процитированные фрагменты с номерами пунктов
func reply_Sources(question string, reply assistantLine) []string {
	sources := []string{question}
	for _, citation := range reply.Citations {
		sources = append(sources, strings.Join([]string{citation.Document, citation.Edition, citation.Section, citation.Paragraph, citation.Appendix, citation.Quote}, "\n"))
	}
	return sources
}

func login_Employee(client *http.Client, apiURL, login, password string) (string, error) {
	var session struct {
		Token string `json:"token"`
	}
	status, err := post_JSON(client, apiURL+"/auth/login", "", map[string]string{
		"login":    login,
		"password": password,
	}, &session)
	if err != nil {
		return "", errors.New(texts.EvalCoreUnavailable)
	}
	if status != http.StatusOK || session.Token == "" {
		return "", errors.New(texts.EvalLoginFailed)
	}
	return session.Token, nil
}

func ask_Question(client *http.Client, apiURL, token, question string) (assistantLine, error) {
	var created struct {
		SessionID string `json:"sessionId"`
	}
	status, err := post_JSON(client, apiURL+"/sessions", token, map[string]string{}, &created)
	if err != nil {
		return assistantLine{}, errors.New(texts.EvalCoreUnavailable)
	}
	if status != http.StatusOK || created.SessionID == "" {
		return assistantLine{}, errors.New(texts.EvalRequestFailed)
	}
	var reply assistantLine
	status, err = post_JSON(client, apiURL+"/sessions/"+created.SessionID+"/messages", token, map[string]string{"text": question}, &reply)
	if err != nil {
		return assistantLine{}, errors.New(texts.EvalCoreUnavailable)
	}
	if status != http.StatusOK {
		return assistantLine{}, errors.New(texts.EvalRequestFailed)
	}
	return reply, nil
}

func post_JSON(client *http.Client, url, token string, body any, dst any) (int, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return response.StatusCode, err
	}
	if dst != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, dst); err != nil {
			return response.StatusCode, err
		}
	}
	return response.StatusCode, nil
}
