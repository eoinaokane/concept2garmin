package concept2

import "testing"

func TestResultStartTime_UsesTimezone(t *testing.T) {
	r := Result{
		Date:     "2026-09-18 12:31:00",
		Timezone: "Europe/Dublin",
	}
	got, err := r.StartTime()
	if err != nil {
		t.Fatalf("StartTime() error = %v", err)
	}
	if got.Hour() != 12 || got.Minute() != 31 {
		t.Errorf("StartTime() = %v, want 12:31 local", got)
	}
	if got.Location().String() != "Europe/Dublin" {
		t.Errorf("Location() = %v, want Europe/Dublin", got.Location())
	}
}

func TestResultStartTime_FallsBackToDateUTC(t *testing.T) {
	r := Result{
		Date:     "not a real date",
		Timezone: "Europe/Dublin",
		DateUTC:  "2026-09-18 12:31:00",
	}
	got, err := r.StartTime()
	if err != nil {
		t.Fatalf("StartTime() error = %v", err)
	}
	if got.Hour() != 12 || got.Minute() != 31 {
		t.Errorf("StartTime() = %v, want 12:31", got)
	}
}

func TestResultStartTime_NoTimezoneUsesDateUTC(t *testing.T) {
	r := Result{
		DateUTC: "2026-09-18 09:00:00",
	}
	got, err := r.StartTime()
	if err != nil {
		t.Fatalf("StartTime() error = %v", err)
	}
	if got.Hour() != 9 {
		t.Errorf("StartTime() = %v, want hour 9", got)
	}
}

func TestResultStartTime_UnparseableReturnsError(t *testing.T) {
	r := Result{}
	if _, err := r.StartTime(); err == nil {
		t.Fatal("StartTime() with no date fields: want error, got nil")
	}
}
