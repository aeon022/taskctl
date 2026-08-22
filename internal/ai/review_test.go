package ai

import "testing"

func TestParseFollowUps(t *testing.T) {
	text := "```json\n[{\"title\":\"Follow up with dentist\",\"due_date\":\"2026-08-25\",\"reason\":\"Appointment call completed this week.\"},{\"title\":\"  \",\"due_date\":\"2026-08-26\",\"reason\":\"blank title should be dropped\"}]\n```"

	got, err := parseFollowUps(text)
	if err != nil {
		t.Fatalf("parseFollowUps: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d suggestions, want 1 (blank-title entry should be dropped): %+v", len(got), got)
	}
	if got[0].Title != "Follow up with dentist" || got[0].DueDate != "2026-08-25" {
		t.Errorf("unexpected suggestion: %+v", got[0])
	}
}

func TestParseFollowUpsEmpty(t *testing.T) {
	got, err := parseFollowUps("[]")
	if err != nil {
		t.Fatalf("parseFollowUps: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d suggestions, want 0", len(got))
	}
}

func TestParseFollowUpsInvalidJSON(t *testing.T) {
	if _, err := parseFollowUps("not json"); err == nil {
		t.Error("expected error for invalid JSON")
	}
}
