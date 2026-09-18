package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eoinaokane/concept2upload/internal/concept2"
)

func TestMachineLabel(t *testing.T) {
	cases := map[string]string{
		"bike":    "Bike",
		"rower":   "Row",
		"skierg":  "SkiErg",
		"dynamic": "DynamicRow",
		"":        "Workout",
		"other":   "Other",
	}
	for c2Type, want := range cases {
		if got := machineLabel(c2Type); got != want {
			t.Errorf("machineLabel(%q) = %q, want %q", c2Type, got, want)
		}
	}
}

func TestCadenceLabel(t *testing.T) {
	if got := cadenceLabel("bike"); got != "Avg cadence (rpm)" {
		t.Errorf("cadenceLabel(bike) = %q", got)
	}
	if got := cadenceLabel("rower"); got != "Avg stroke rate (spm)" {
		t.Errorf("cadenceLabel(rower) = %q", got)
	}
	if got := cadenceLabel("dynamic"); got != "Avg stroke rate (spm)" {
		t.Errorf("cadenceLabel(dynamic) = %q", got)
	}
}

// TestActivityTypeFromFileName covers every machineLabel-derived file name
// prefix, including "dynamic" (DynamicRow) - see issue about the "dynamic"
// machine type having no test coverage at all.
func TestActivityTypeFromFileName(t *testing.T) {
	cases := map[string]string{
		"2026-09-18-1231-Bike-30min-13.1km.tcx":      "ride",
		"2026-09-18-1231-Row-30min-8.0km.tcx":        "rowing",
		"2026-09-18-1231-DynamicRow-30min-8.0km.tcx": "rowing",
		"2026-09-18-1231-SkiErg-30min-8.0km.tcx":     "workout",
		"2026-09-18-1231-Workout-30min-8.0km.tcx":    "workout",
	}
	for name, want := range cases {
		if got := activityTypeFromFileName(name); got != want {
			t.Errorf("activityTypeFromFileName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestValueOr(t *testing.T) {
	if got := valueOr("", "n/a"); got != "n/a" {
		t.Errorf("valueOr(\"\", n/a) = %q, want n/a", got)
	}
	if got := valueOr("  ", "n/a"); got != "n/a" {
		t.Errorf("valueOr(whitespace, n/a) = %q, want n/a", got)
	}
	if got := valueOr("hello", "n/a"); got != "hello" {
		t.Errorf("valueOr(hello, n/a) = %q, want hello", got)
	}
}

func TestIntOr(t *testing.T) {
	if got := intOr(0, "n/a"); got != "n/a" {
		t.Errorf("intOr(0, n/a) = %q, want n/a", got)
	}
	if got := intOr(42, "n/a"); got != "42" {
		t.Errorf("intOr(42, n/a) = %q, want 42", got)
	}
}

func TestYesNo(t *testing.T) {
	if got := yesNo(true); got != "yes" {
		t.Errorf("yesNo(true) = %q", got)
	}
	if got := yesNo(false); got != "no" {
		t.Errorf("yesNo(false) = %q", got)
	}
}

func TestParsePosition(t *testing.T) {
	cases := []struct {
		arg     string
		want    int
		wantErr bool
	}{
		{"1", 1, false},
		{"42", 42, false},
		{"0", 0, true},
		{"-3", 0, true},
		{"abc", 0, true},
	}
	for _, c := range cases {
		got, err := parsePosition(c.arg)
		if c.wantErr {
			if err == nil {
				t.Errorf("parsePosition(%q): want error, got nil", c.arg)
			}
			continue
		}
		if err != nil {
			t.Errorf("parsePosition(%q): unexpected error %v", c.arg, err)
		}
		if got != c.want {
			t.Errorf("parsePosition(%q) = %d, want %d", c.arg, got, c.want)
		}
	}
}

func TestElapsedMinutes(t *testing.T) {
	cases := []struct {
		formatted string
		rawTenths int
		want      float64
	}{
		{"30:00.0", 0, 30},
		{"1:02:30.0", 0, 62.5},
		{"", 1800, 3}, // falls back to rawTenths: 1800 tenths = 180s = 3min
	}
	for _, c := range cases {
		got := elapsedMinutes(c.formatted, c.rawTenths)
		if diff := got - c.want; diff > 0.001 || diff < -0.001 {
			t.Errorf("elapsedMinutes(%q, %d) = %v, want %v", c.formatted, c.rawTenths, got, c.want)
		}
	}
}

func TestSensibleFileName(t *testing.T) {
	detail := concept2.ResultDetail{
		Result: concept2.Result{
			Date:          "2026-09-18 12:31:00",
			Timezone:      "UTC",
			TimeFormatted: "30:00.0",
			Distance:      13079,
			Type:          "bike",
		},
	}
	got := sensibleFileName(detail)
	want := "2026-09-18-1231-Bike-30min-13.1km.tcx"
	if got != want {
		t.Errorf("sensibleFileName() = %q, want %q", got, want)
	}
}

func TestUniqueFilePath(t *testing.T) {
	dir := t.TempDir()

	first := uniqueFilePath(dir, "workout.tcx")
	if first != filepath.Join(dir, "workout.tcx") {
		t.Errorf("first call = %q, want workout.tcx", first)
	}

	if err := os.WriteFile(first, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	second := uniqueFilePath(dir, "workout.tcx")
	if second != filepath.Join(dir, "workout-1.tcx") {
		t.Errorf("second call = %q, want workout-1.tcx", second)
	}
}

func TestHeartRateSummary(t *testing.T) {
	// Prefers the top-level summary when present.
	hr := concept2.HeartRate{Average: 150, Min: 120, Max: 180}
	got := heartRateSummary(hr, nil)
	want := "avg 150, min 120, max 180 bpm"
	if got != want {
		t.Errorf("heartRateSummary(summary) = %q, want %q", got, want)
	}

	// Falls back to deriving from segment "ending" heart rates.
	segments := []concept2.WorkoutSegment{
		{HeartRate: concept2.HeartRate{Ending: 140}},
		{HeartRate: concept2.HeartRate{Ending: 160}},
	}
	got = heartRateSummary(concept2.HeartRate{}, segments)
	want = "avg 150, min 140, max 160 bpm"
	if got != want {
		t.Errorf("heartRateSummary(segments) = %q, want %q", got, want)
	}

	// No heart rate data anywhere.
	if got := heartRateSummary(concept2.HeartRate{}, nil); got != "n/a" {
		t.Errorf("heartRateSummary(none) = %q, want n/a", got)
	}
}
