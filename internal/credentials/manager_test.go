package credentials

import (
	"errors"
	"testing"
)

type fakeStore struct {
	values           map[string]string
	failWriteTarget  string
	failDeleteTarget string
}

func newFakeStore() *fakeStore {
	return &fakeStore{values: map[string]string{}}
}

func (s *fakeStore) Read(target string) (string, bool, error) {
	value, ok := s.values[target]
	return value, ok, nil
}

func (s *fakeStore) Write(target, value string) error {
	if target == s.failWriteTarget {
		return errors.New("forced write failure")
	}
	s.values[target] = value
	return nil
}

func (s *fakeStore) Delete(target string) error {
	if target == s.failDeleteTarget {
		return errors.New("forced delete failure")
	}
	delete(s.values, target)
	return nil
}

func TestValidatePairRejectsWrongTokenKinds(t *testing.T) {
	if err := ValidatePair(Pair{AppToken: "xoxb-wrong", BotToken: "xoxb-good"}); err == nil {
		t.Fatal("expected app token validation failure")
	}
	if err := ValidatePair(Pair{AppToken: "xapp-good", BotToken: "xapp-wrong"}); err == nil {
		t.Fatal("expected bot token validation failure")
	}
}

func TestReplacePairRollsBackExactPreviousPair(t *testing.T) {
	store := newFakeStore()
	store.values[SlackAppTokenTarget] = "xapp-old"
	store.values[SlackBotTokenTarget] = "xoxb-old"
	manager := newWithStore(store)
	store.failWriteTarget = SlackBotTokenTarget

	_, err := manager.ReplacePair(Pair{AppToken: "xapp-new", BotToken: "xoxb-new"})
	if err == nil {
		t.Fatal("expected replace failure")
	}
	if got := store.values[SlackAppTokenTarget]; got != "xapp-old" {
		t.Fatalf("app token rollback = %q", got)
	}
	if got := store.values[SlackBotTokenTarget]; got != "xoxb-old" {
		t.Fatalf("bot token rollback = %q", got)
	}
}

func TestReplacePairRollsBackPreviousAbsence(t *testing.T) {
	store := newFakeStore()
	manager := newWithStore(store)
	store.failWriteTarget = SlackBotTokenTarget

	_, err := manager.ReplacePair(Pair{AppToken: "xapp-new", BotToken: "xoxb-new"})
	if err == nil {
		t.Fatal("expected replace failure")
	}
	if _, ok := store.values[SlackAppTokenTarget]; ok {
		t.Fatal("app token should be absent after rollback")
	}
	if _, ok := store.values[SlackBotTokenTarget]; ok {
		t.Fatal("bot token should be absent after rollback")
	}
}

func TestStatusExposesPresenceOnly(t *testing.T) {
	store := newFakeStore()
	store.values[SlackAppTokenTarget] = "xapp-private"
	store.values[SlackBotTokenTarget] = "xoxb-private"
	manager := newWithStore(store)

	status, err := manager.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !status.AppTokenPresent || !status.BotTokenPresent || status.Store != StoreName {
		t.Fatalf("status = %#v", status)
	}
}
