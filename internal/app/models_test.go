package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"tongxi/internal/store"
)

func modelInput(mID string, version int, baseURL, key string) ModelInput {
	return ModelInput{ID: mID, Version: version, Name: "Test", Provider: "compatible", BaseURL: baseURL, Model: "test", APIKey: key}
}

func TestModelContextCapacityValidationAndPersistence(t *testing.T) {
	s := newService(t)
	in := modelInput("", 0, "https://example.test/v1", "test-secret")
	for _, capacity := range []int{4096, 2000001} {
		in.ContextTokens = capacity
		if _, err := s.SaveModel(in); err == nil {
			t.Fatal("invalid capacity accepted", capacity)
		}
	}
	in.ContextTokens = 65536
	m, err := s.SaveModel(in)
	if err != nil || m.ContextTokens != 65536 {
		t.Fatal("capacity not saved", err)
	}
	got, err := s.db.Model(m.ID)
	if err != nil || got.ContextTokens != 65536 {
		t.Fatal("capacity not loaded", err)
	}
}

func TestModelCredentialsRotationPrivacyAndValidation(t *testing.T) {
	s := newService(t)
	in := modelInput("", 0, "https://example.test/v1/", "secret-one")
	m, err := s.SaveModel(in)
	if err != nil {
		t.Fatal(err)
	}
	other := saveTestModel(t, s, "other", "secret-two")
	a, err := s.SaveAgent(AgentInput{Name: "Agent", Instruction: "test", ModelID: m.ID})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.db.Model(m.ID)
	in.ID, in.Version, in.APIKey, in.BaseURL = m.ID, m.Version, "", "https://other.test/v1"
	if _, err = s.SaveModel(in); err == nil {
		t.Fatal("save reused key for new endpoint")
	}
	if _, err = s.TestModel(in); err == nil {
		t.Fatal("test reused key for new endpoint")
	}
	in.BaseURL = "https://example.test/v1"
	in.APIKey = "secret-rotated"
	m, err = s.SaveModel(in)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := s.db.Agent(a.ID)
	if after.KeyRef == before.KeyRef {
		t.Fatal("role did not inherit changed model")
	}
	if _, err = s.vault.Get(before.KeyRef); err == nil {
		t.Fatal("unused key not removed")
	}
	otherPersisted, _ := s.db.Model(other.ID)
	if key, err := s.vault.Get(otherPersisted.KeyRef); err != nil || key != "secret-two" {
		t.Fatal("other model changed")
	}
	all, _ := s.Models()
	data, _ := json.Marshal(all)
	for _, secret := range []string{"secret-one", "secret-two", "secret-rotated", after.KeyRef} {
		if strings.Contains(string(data), secret) {
			t.Fatal("secret returned")
		}
	}
	if err = s.DeleteModel(m.ID, m.Version); err == nil {
		t.Fatal("model referenced by role was deleted")
	}
	if err = s.DeleteModel(other.ID, other.Version); err != nil {
		t.Fatal(err)
	}
	if _, err = s.vault.Get(otherPersisted.KeyRef); err == nil {
		t.Fatal("deleted model key retained")
	}
}

func TestConnectionUsesDraftWithoutToolsPersistenceOrRedirects(t *testing.T) {
	s := newService(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer temporary-key" {
			t.Error("wrong endpoint or credential")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["model"] != "test" || body["tools"] != nil || body["stream"] != true {
			t.Error("probe was not a minimal stream", body)
		}
		if messages, ok := body["messages"].([]any); !ok || len(messages) != 1 {
			t.Error("unexpected history")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"id":"probe","choices":[{"index":0,"delta":{"role":"assistant","content":"OK"}}]}`)
		fmt.Fprintln(w)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	result, err := s.TestModel(modelInput("", 0, server.URL+"/v1", "temporary-key"))
	if err != nil || result.LatencyMS < 0 || requests.Load() != 1 {
		t.Fatal(result, err, requests.Load())
	}
	configs, _ := s.Models()
	probes, _ := s.db.Runs()
	if len(configs) != 0 || len(probes) != 0 || len(s.vault.(memoryVault)) != 0 {
		t.Fatal("test persisted draft, key or history")
	}
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	if _, err = s.TestModel(modelInput("", 0, redirect.URL, "temporary-key")); err == nil || redirected.Load() != 0 {
		t.Fatal("followed model redirect", err)
	}
}

func TestConnectionErrorsHideSecretsAndNeverClaimEmptyStreamSuccess(t *testing.T) {
	for _, mode := range []string{"unauthorized", "empty"} {
		t.Run(mode, func(t *testing.T) {
			s := newService(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "empty" {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: [DONE]\n\n")
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				fmt.Fprint(w, `{"error":{"message":"bad key private-token","type":"invalid_api_key","code":"invalid_api_key"}}`)
			}))
			defer server.Close()
			_, err := s.TestModel(modelInput("", 0, server.URL, "private-token"))
			if err == nil || strings.Contains(err.Error(), "private-token") {
				t.Fatal("wrong error", err)
			}
		})
	}
}

func TestConnectionReadsSavedKeyAndCloseCancelsPendingRequest(t *testing.T) {
	s := newService(t)
	m := saveTestModel(t, s, "test", "saved-secret")
	started := make(chan struct{})
	done := make(chan error, 1)
	s.newChatModel = func(ctx context.Context, base, name, key string) (model.ToolCallingChatModel, error) {
		if key != "saved-secret" {
			t.Error("saved key was not resolved")
		}
		return &scriptedModel{stream: func(ctx context.Context, _ []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		}}, nil
	}
	go func() { _, err := s.TestModel(modelInput(m.ID, m.Version, m.BaseURL, "")); done <- err }()
	<-started
	if _, err := s.TestModel(modelInput(m.ID, m.Version, m.BaseURL, "")); err == nil {
		t.Fatal("duplicate test accepted")
	}
	s.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled request succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel test")
	}
}

func TestSchedulerUsesEachRolesSelectedModel(t *testing.T) {
	s := newService(t)
	calls := make(chan string, 4)
	s.newChatModel = func(ctx context.Context, base, name, key string) (model.ToolCallingChatModel, error) {
		calls <- name + ":" + key
		return &scriptedModel{stream: func(context.Context, []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
			r, w := schema.Pipe[*schema.Message](1)
			w.Send(schema.AssistantMessage("reply", nil), nil)
			w.Close()
			return r, nil
		}}, nil
	}
	events := make(chan store.ConversationRun, 100)
	s.StartScheduler(func(r store.ConversationRun) { events <- r })
	for i, name := range []string{"writer-model", "review-model"} {
		m := saveTestModel(t, s, name, name+"-secret")
		a, err := s.SaveAgent(AgentInput{Name: name, Instruction: "test", ModelID: m.ID})
		if err != nil {
			t.Fatal(err)
		}
		c, err := s.SaveConversation(store.Conversation{Title: name, Kind: "private", Mode: "lead", LeadAgentID: a.ID, MemberIDs: []string{a.ID}})
		if err != nil {
			t.Fatal(err)
		}
		r, err := s.SendMessage(c.ID, "hello", fmt.Sprintf("model-routing-%016d", i))
		if err != nil {
			t.Fatal(err)
		}
		waitRun(t, events, r.ID, "completed")
		if got := <-calls; got != name+":"+name+"-secret" {
			t.Fatal("wrong model routed", got)
		}
	}
}

func TestModelSaveFailureRemovesOnlyNewCredential(t *testing.T) {
	s := newService(t)
	saved := saveTestModel(t, s, "keep", "keep-secret")
	persisted, _ := s.db.Model(saved.ID)
	s.db.Close()
	if _, err := s.SaveModel(modelInput("", 0, "https://example.test/v1", "discard-secret")); err == nil {
		t.Fatal("storage failure succeeded")
	}
	vault := s.vault.(memoryVault)
	if len(vault) != 1 || vault[persisted.KeyRef] != "keep-secret" {
		t.Fatal("failed save left a credential or removed an existing one")
	}
}
