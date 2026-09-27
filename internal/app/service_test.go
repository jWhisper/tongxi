package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"tongxi/internal/agent"
	"tongxi/internal/store"
)

type memoryVault map[string]string

func (v memoryVault) Get(ref string) (string, error) {
	s, ok := v[ref]
	if !ok {
		return "", errors.New("missing")
	}
	return s, nil
}
func (v memoryVault) Set(ref, key string) error { v[ref] = key; return nil }
func (v memoryVault) Delete(ref string) error   { delete(v, ref); return nil }

func newService(t *testing.T) *Service {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(context.Background(), db, memoryVault{}, func(store.ProbeRun) {})
	t.Cleanup(s.Close)
	return s
}

func TestSettingsNeverReturnSecretAndEndpointChangeRequiresKey(t *testing.T) {
	s := newService(t)
	input := SettingsInput{BaseURL: "https://example.test/v1/", Model: "test", APIKey: "sensitive-test-value"}
	v, err := s.SaveSettings(input)
	if err != nil {
		t.Fatal(err)
	}
	if !v.HasKey || v.BaseURL != "https://example.test/v1" {
		t.Fatalf("unexpected view %+v", v)
	}
	snapshot, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(snapshot)
	if strings.Contains(string(data), input.APIKey) {
		t.Fatal("snapshot leaked secret")
	}
	persisted, _ := s.db.Settings()
	if persisted.KeyRef == input.APIKey {
		t.Fatal("database contains plaintext secret")
	}
	input.APIKey = ""
	input.BaseURL = "https://other.test/v1"
	if _, err = s.SaveSettings(input); err == nil {
		t.Fatal("existing secret could be sent to a new endpoint without re-entry")
	}
	input.BaseURL = "http://example.test/v1"
	input.APIKey = "test"
	if _, err = s.SaveSettings(input); err == nil {
		t.Fatal("remote plaintext HTTP accepted")
	}
}

func TestStopRejectsLateOutputAndCompletion(t *testing.T) {
	s := newService(t)
	started, release := make(chan struct{}), make(chan struct{})
	s.execute = func(ctx context.Context, _ model.ToolCallingChatModel, _ string, emit func(agent.Update)) error {
		emit(agent.Update{Text: "partial"})
		close(started)
		<-ctx.Done()
		<-release
		emit(agent.Update{Text: "late output"})
		return nil
	}
	r, err := s.StartProbe("local", "")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("not started")
	}
	if _, err = s.StartProbe("local", ""); err == nil {
		t.Fatal("concurrent probe accepted")
	}
	if err = s.StopProbe(r.ID); err != nil {
		t.Fatal(err)
	}
	close(release)
	s.wg.Wait()
	snap, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	got := snap.Runs[0]
	if got.Status != "cancelled" || got.Text != "partial" {
		t.Fatalf("late event corrupted result %+v", got)
	}
	persisted, _ := s.db.Runs()
	if persisted[0].Status != "cancelled" {
		t.Fatal("cancel was not durable")
	}
}

func TestFailureIsSavedWithSecretRedacted(t *testing.T) {
	s := newService(t)
	if _, err := s.SaveSettings(SettingsInput{BaseURL: "https://example.test/v1", Model: "test", APIKey: "private-key"}); err != nil {
		t.Fatal(err)
	}
	s.execute = func(context.Context, model.ToolCallingChatModel, string, func(agent.Update)) error {
		return errors.New("provider echoed private-key")
	}
	if _, err := s.StartProbe("model", "hello"); err != nil {
		t.Fatal(err)
	}
	s.wg.Wait()
	snap, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	r := snap.Runs[0]
	if r.Status != "failed" || strings.Contains(r.Error, "private-key") || !strings.Contains(r.Error, "[已隐藏]") {
		t.Fatalf("unexpected failure %+v", r)
	}
}

func TestSystemVaultRoundTrip(t *testing.T) {
	if os.Getenv("TONGXI_TEST_KEYRING") != "1" {
		t.Skip("set TONGXI_TEST_KEYRING=1 to verify the OS credential store")
	}
	v := SystemVault{}
	ref := "test-" + newID()
	if err := v.Set(ref, "tongxi-non-secret-probe"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v.Delete(ref) })
	got, err := v.Get(ref)
	if err != nil || got != "tongxi-non-secret-probe" {
		t.Fatalf("credential store round trip failed: %v", err)
	}
	if err = v.Delete(ref); err != nil {
		t.Fatal(err)
	}
	if _, err = v.Get(ref); err == nil {
		t.Fatal("test credential not removed")
	}
}
