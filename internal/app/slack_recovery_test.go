package app

import "testing"

func TestRecoveredHistoryOnlyDispatchesMessagesAfterCursor(t *testing.T) {
	if shouldDispatchRecoveredMessage("1.000", "2.000") {
		t.Fatal("recovery must not dispatch history before the current session cursor")
	}
	if shouldDispatchRecoveredMessage("2.000", "2.000") {
		t.Fatal("recovery must not dispatch the cursor message again")
	}
	if !shouldDispatchRecoveredMessage("3.000", "2.000") {
		t.Fatal("recovery lost a message arriving after the cursor")
	}
}
