package tcx

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/eoinaokane/concept2garmin/internal/concept2"
)

func TestWattsFromPace(t *testing.T) {
	cases := []struct {
		name string
		pace int
		want int
	}{
		{"2:00/500m", 1200, 203},
		{"1:30/500m", 900, 480},
		{"zero", 0, 0},
		{"negative", -5, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := WattsFromPace(c.pace); got != c.want {
				t.Errorf("WattsFromPace(%d) = %d, want %d", c.pace, got, c.want)
			}
		})
	}
}

func TestWattsFromDistanceTime(t *testing.T) {
	// 2000m rowed in 8:00 (4800 tenths) is a 2:00/500m pace, same as the
	// WattsFromPace case above.
	if got, want := WattsFromDistanceTime(2000, 4800, 500), 203; got != want {
		t.Errorf("WattsFromDistanceTime(2000, 4800, 500) = %d, want %d", got, want)
	}
	cases := []struct {
		name                                    string
		distanceMetres, timeTenths, splitMetres int
	}{
		{"zero distance", 0, 4800, 500},
		{"zero time", 2000, 0, 500},
		{"zero split", 2000, 4800, 0},
		{"negative distance", -1, 4800, 500},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := WattsFromDistanceTime(c.distanceMetres, c.timeTenths, c.splitMetres); got != 0 {
				t.Errorf("WattsFromDistanceTime(%d, %d, %d) = %d, want 0", c.distanceMetres, c.timeTenths, c.splitMetres, got)
			}
		})
	}
}

func TestSplitDistanceMetres(t *testing.T) {
	cases := map[string]int{
		"bike":    1000,
		"rower":   500,
		"skierg":  500,
		"dynamic": 500,
		"":        500,
	}
	for c2Type, want := range cases {
		if got := SplitDistanceMetres(c2Type); got != want {
			t.Errorf("SplitDistanceMetres(%q) = %d, want %d", c2Type, got, want)
		}
	}
}

func TestClampByte(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{-1, 0},
		{0, 0},
		{254, 254},
		{255, 254},
		{1000, 254},
		{100, 100},
	}
	for _, c := range cases {
		if got := clampByte(c.in); got != c.want {
			t.Errorf("clampByte(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestSportFor(t *testing.T) {
	if got := sportFor("bike"); got != "Biking" {
		t.Errorf("sportFor(bike) = %q, want Biking", got)
	}
	if got := sportFor("rower"); got != "Other" {
		t.Errorf("sportFor(rower) = %q, want Other", got)
	}
	if got := sportFor("skierg"); got != "Other" {
		t.Errorf("sportFor(skierg) = %q, want Other", got)
	}
}

func TestBuildNotes(t *testing.T) {
	if got, want := buildNotes(""), "Exported from Concept2 via "+SourceURL; got != want {
		t.Errorf("buildNotes(\"\") = %q, want %q", got, want)
	}
	got := buildNotes("Great session")
	if !strings.HasPrefix(got, "Great session\n\n") || !strings.HasSuffix(got, SourceURL) {
		t.Errorf("buildNotes(\"Great session\") = %q, missing comment or attribution", got)
	}
}

func TestHeartRateStats(t *testing.T) {
	points := []point{
		{HeartRateBpm: 140},
		{HeartRateBpm: 160},
		{HeartRateBpm: 0}, // ignored
		{HeartRateBpm: 150},
	}
	avg, max := heartRateStats(points)
	if avg != 150 {
		t.Errorf("avg = %d, want 150", avg)
	}
	if max != 160 {
		t.Errorf("max = %d, want 160", max)
	}

	if avg, max := heartRateStats(nil); avg != 0 || max != 0 {
		t.Errorf("heartRateStats(nil) = (%d, %d), want (0, 0)", avg, max)
	}
}

func TestAvgCadence(t *testing.T) {
	points := []point{{Cadence: 20}, {Cadence: 0}, {Cadence: 30}}
	if got := avgCadence(points); got != 25 {
		t.Errorf("avgCadence = %d, want 25", got)
	}
	if got := avgCadence(nil); got != 0 {
		t.Errorf("avgCadence(nil) = %d, want 0", got)
	}
}

// TestLapsFromStrokes_SingleInterval checks that stroke data with no time
// reset (a continuous piece) collapses into a single lap covering the whole
// set of strokes.
func TestLapsFromStrokes_SingleInterval(t *testing.T) {
	start := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	strokes := []concept2.Stroke{
		{Time: 10, Distance: 20, Pace: 1200, StrokeRate: 24, HeartRate: 140},
		{Time: 20, Distance: 40, Pace: 1200, StrokeRate: 24, HeartRate: 145},
		{Time: 30, Distance: 60, Pace: 1200, StrokeRate: 24, HeartRate: 150},
	}

	laps := lapsFromStrokes(strokes, start)
	if len(laps) != 1 {
		t.Fatalf("len(laps) = %d, want 1", len(laps))
	}
	if laps[0].TimeTenths != 30 {
		t.Errorf("TimeTenths = %d, want 30", laps[0].TimeTenths)
	}
	if laps[0].DistanceMeters != 6.0 {
		t.Errorf("DistanceMeters = %v, want 6.0", laps[0].DistanceMeters)
	}
	if len(laps[0].Points) != 3 {
		t.Errorf("len(Points) = %d, want 3", len(laps[0].Points))
	}
}

// TestLapsFromStrokes_MultipleIntervals checks that a time reset in the
// cumulative stroke data (as Concept2 reports for interval workouts with
// rest) splits the strokes into separate laps, with totals continuing to
// climb across the whole workout.
func TestLapsFromStrokes_MultipleIntervals(t *testing.T) {
	start := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	strokes := []concept2.Stroke{
		// interval 1: 0 -> 20 tenths, 0 -> 40 decimetres
		{Time: 10, Distance: 20, Pace: 1200},
		{Time: 20, Distance: 40, Pace: 1200},
		// interval 2 starts: time/distance reset to near zero
		{Time: 5, Distance: 10, Pace: 1200},
		{Time: 15, Distance: 30, Pace: 1200},
	}

	laps := lapsFromStrokes(strokes, start)
	if len(laps) != 2 {
		t.Fatalf("len(laps) = %d, want 2", len(laps))
	}

	if laps[0].TimeTenths != 20 {
		t.Errorf("lap 0 TimeTenths = %d, want 20", laps[0].TimeTenths)
	}
	if laps[0].DistanceMeters != 4.0 {
		t.Errorf("lap 0 DistanceMeters = %v, want 4.0", laps[0].DistanceMeters)
	}

	if laps[1].TimeTenths != 15 {
		t.Errorf("lap 1 TimeTenths = %d, want 15", laps[1].TimeTenths)
	}
	if laps[1].DistanceMeters != 3.0 {
		t.Errorf("lap 1 DistanceMeters = %v, want 3.0", laps[1].DistanceMeters)
	}
	if laps[1].TimeTenths < 0 || laps[1].DistanceMeters < 0 {
		t.Fatalf("lap 1 has negative totals: %+v (interval offset carried forward incorrectly)", laps[1])
	}

	// Absolute timestamps/distances must keep climbing across the lap
	// boundary rather than resetting with the raw stroke data.
	lastPointLap0 := laps[0].Points[len(laps[0].Points)-1]
	firstPointLap1 := laps[1].Points[0]
	if !firstPointLap1.Time.After(lastPointLap0.Time) {
		t.Errorf("lap 1 first point time %v not after lap 0 last point time %v", firstPointLap1.Time, lastPointLap0.Time)
	}
	if firstPointLap1.DistanceMeters <= lastPointLap0.DistanceMeters {
		t.Errorf("lap 1 first point distance %v not greater than lap 0 last point distance %v", firstPointLap1.DistanceMeters, lastPointLap0.DistanceMeters)
	}
}

func TestLapsFromSegments(t *testing.T) {
	start := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	segments := []concept2.WorkoutSegment{
		{Time: 300, Distance: 1000, CaloriesTotal: 20, StrokeRate: 22, HeartRate: concept2.HeartRate{Ending: 140}},
		{Time: 600, Distance: 2000, CaloriesTotal: 40, StrokeRate: 24, HeartRate: concept2.HeartRate{Ending: 155}},
	}

	laps := lapsFromSegments(segments, start, 500)
	if len(laps) != 2 {
		t.Fatalf("len(laps) = %d, want 2", len(laps))
	}

	if laps[0].TimeTenths != 300 || laps[0].DistanceMeters != 1000 || laps[0].Calories != 20 {
		t.Errorf("lap 0 = %+v, unexpected totals", laps[0])
	}
	if laps[1].TimeTenths != 600 || laps[1].DistanceMeters != 2000 || laps[1].Calories != 40 {
		t.Errorf("lap 1 = %+v, unexpected totals", laps[1])
	}

	// Every point should carry the segment's constant HR/cadence/watts.
	for _, p := range laps[0].Points {
		if p.HeartRateBpm != 140 {
			t.Errorf("lap 0 point HR = %d, want 140", p.HeartRateBpm)
		}
		if p.Cadence != 22 {
			t.Errorf("lap 0 point cadence = %d, want 22", p.Cadence)
		}
	}

	// The second lap's points should start where the first lap's distance
	// left off (cumulative distance carried forward across laps).
	firstPointLap1 := laps[1].Points[0]
	if firstPointLap1.DistanceMeters != 1000 {
		t.Errorf("lap 1 first point distance = %v, want 1000", firstPointLap1.DistanceMeters)
	}
}

func TestBuild_SynthesizesSingleLapWithoutSegmentsOrStrokes(t *testing.T) {
	detail := concept2.ResultDetail{
		Result: concept2.Result{
			ID:            1,
			Date:          "2026-09-18 12:00:00",
			DateUTC:       "2026-09-18 12:00:00",
			Distance:      2000,
			Time:          4800,
			CaloriesTotal: 200,
			Type:          "rower",
			HeartRate:     concept2.HeartRate{Average: 150},
		},
	}

	body, err := Build(detail)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	var doc trainingCenterDatabase
	if err := xml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("output is not well-formed XML: %v", err)
	}

	if got := len(doc.Activities.Activity.Laps); got != 1 {
		t.Fatalf("len(Laps) = %d, want 1", got)
	}
	lap := doc.Activities.Activity.Laps[0]
	if lap.DistanceMeters != 2000 {
		t.Errorf("DistanceMeters = %v, want 2000", lap.DistanceMeters)
	}
	if got := doc.Activities.Activity.Sport; got != "Other" {
		t.Errorf("Sport = %q, want Other", got)
	}
}

func TestBuild_OneLapPerInterval(t *testing.T) {
	detail := concept2.ResultDetail{
		Result: concept2.Result{
			ID:            2,
			Date:          "2026-09-18 12:00:00",
			DateUTC:       "2026-09-18 12:00:00",
			Distance:      2000,
			Time:          4800,
			CaloriesTotal: 200,
			Type:          "bike",
			Workout: concept2.Workout{
				Intervals: []concept2.WorkoutSegment{
					{Time: 2400, Distance: 1000, CaloriesTotal: 100},
					{Time: 2400, Distance: 1000, CaloriesTotal: 100},
				},
			},
		},
	}

	body, err := Build(detail)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	var doc trainingCenterDatabase
	if err := xml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("output is not well-formed XML: %v", err)
	}

	if got := len(doc.Activities.Activity.Laps); got != 2 {
		t.Fatalf("len(Laps) = %d, want 2", got)
	}
	if got := doc.Activities.Activity.Sport; got != "Biking" {
		t.Errorf("Sport = %q, want Biking", got)
	}

	// Second lap must start strictly after the first lap's start time.
	t1, err := time.Parse(time.RFC3339, doc.Activities.Activity.Laps[0].StartTime)
	if err != nil {
		t.Fatalf("parsing lap 0 start time: %v", err)
	}
	t2, err := time.Parse(time.RFC3339, doc.Activities.Activity.Laps[1].StartTime)
	if err != nil {
		t.Fatalf("parsing lap 1 start time: %v", err)
	}
	if !t2.After(t1) {
		t.Errorf("lap 1 start %v not after lap 0 start %v", t2, t1)
	}
}

func TestBuild_MissingStartTimeErrors(t *testing.T) {
	detail := concept2.ResultDetail{Result: concept2.Result{ID: 3}}
	if _, err := Build(detail); err == nil {
		t.Fatal("Build with no parseable date: want error, got nil")
	}
}
