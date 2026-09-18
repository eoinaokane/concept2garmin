package tcx

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/eoinaokane/concept2upload/internal/concept2"
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
	if got := sportFor("dynamic"); got != "Other" {
		t.Errorf("sportFor(dynamic) = %q, want Other", got)
	}
}

func TestBuildNotes(t *testing.T) {
	detail := concept2.ResultDetail{
		Result: concept2.Result{
			WorkoutType:   "VariableInterval",
			Distance:      13079,
			Time:          18000,
			TimeFormatted: "30:00.0",
			Type:          "bike",
			HeartRate:     concept2.HeartRate{Average: 142},
		},
	}
	got := buildNotes(detail)
	for _, want := range []string{"VariableInterval", "13.1 km", "30:00.0", "avg", "W", "avg HR 142 bpm"} {
		if !strings.Contains(got, want) {
			t.Errorf("buildNotes result missing %q: %q", want, got)
		}
	}
	if !strings.HasSuffix(got, SourceURL) {
		t.Errorf("buildNotes result missing attribution: %q", got)
	}

	detail.Comments = "Great session"
	got = buildNotes(detail)
	if !strings.Contains(got, "Great session") {
		t.Errorf("buildNotes result missing comment: %q", got)
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

	laps := lapsFromStrokes(strokes, start, nil)
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

	laps := lapsFromStrokes(strokes, start, nil)
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

func TestCumulativeDeciBoundaries(t *testing.T) {
	segments := []concept2.WorkoutSegment{
		{Distance: 500},
		{Distance: 500},
		{Distance: 1000},
	}
	got := cumulativeDeciBoundaries(segments)
	want := []int{5000, 10000, 20000}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("boundary[%d] = %d, want %d", i, got[i], want[i])
		}
	}

	if got := cumulativeDeciBoundaries(nil); got != nil {
		t.Errorf("cumulativeDeciBoundaries(nil) = %v, want nil", got)
	}
}

// TestLapsFromStrokes_FixedDistanceSplits checks the fix for a
// FixedDistanceSplits workout: a single continuous piece (stroke time never
// resets) but with distance markers from Workout.Splits. Without the
// splitBoundariesDeci argument, this would collapse into a single lap even
// though Concept2 reports multiple splits for it.
func TestLapsFromStrokes_FixedDistanceSplits(t *testing.T) {
	start := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	// Continuous 1000m piece split at 500m: cumulative time/distance never
	// resets across the split boundary. Distance is in decimetres (matching
	// concept2.Stroke.Distance), so 500m = 5000.
	strokes := []concept2.Stroke{
		{Time: 1000, Distance: 4900, Pace: 1200},
		{Time: 1200, Distance: 5000, Pace: 1200}, // crosses the 500m (5000 deci) boundary
		{Time: 2200, Distance: 9900, Pace: 1200},
		{Time: 2400, Distance: 10000, Pace: 1200},
	}
	boundaries := cumulativeDeciBoundaries([]concept2.WorkoutSegment{
		{Distance: 500},
		{Distance: 500},
	})

	laps := lapsFromStrokes(strokes, start, boundaries)
	if len(laps) != 2 {
		t.Fatalf("len(laps) = %d, want 2", len(laps))
	}

	if laps[0].DistanceMeters != 500.0 {
		t.Errorf("lap 0 DistanceMeters = %v, want 500.0", laps[0].DistanceMeters)
	}
	if laps[0].TimeTenths != 1200 {
		t.Errorf("lap 0 TimeTenths = %d, want 1200", laps[0].TimeTenths)
	}

	if laps[1].DistanceMeters != 500.0 {
		t.Errorf("lap 1 DistanceMeters = %v, want 500.0", laps[1].DistanceMeters)
	}
	if laps[1].TimeTenths != 1200 {
		t.Errorf("lap 1 TimeTenths = %d, want 1200", laps[1].TimeTenths)
	}

	// Distances/times must keep climbing across the split boundary, not
	// reset the way an interval boundary does.
	lastPointLap0 := laps[0].Points[len(laps[0].Points)-1]
	firstPointLap1 := laps[1].Points[0]
	if firstPointLap1.DistanceMeters <= lastPointLap0.DistanceMeters {
		t.Errorf("lap 1 first point distance %v not greater than lap 0 last point distance %v", firstPointLap1.DistanceMeters, lastPointLap0.DistanceMeters)
	}
}

func TestBuild_FixedDistanceSplitsProducesMultipleLaps(t *testing.T) {
	detail := concept2.ResultDetail{
		Result: concept2.Result{
			ID:            4,
			Date:          "2026-09-18 12:00:00",
			Timezone:      "UTC",
			Distance:      1000,
			Time:          2400,
			CaloriesTotal: 20,
			Type:          "rower",
			Workout: concept2.Workout{
				// No Intervals - a FixedDistanceSplits workout only reports
				// Splits, with no rest between them.
				Splits: []concept2.WorkoutSegment{
					{Time: 1200, Distance: 500, CaloriesTotal: 10},
					{Time: 1200, Distance: 500, CaloriesTotal: 10},
				},
			},
		},
	}
	detail.Strokes.Data = []concept2.Stroke{
		{Time: 1000, Distance: 4900, Pace: 1200},
		{Time: 1200, Distance: 5000, Pace: 1200},
		{Time: 2200, Distance: 9900, Pace: 1200},
		{Time: 2400, Distance: 10000, Pace: 1200},
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
}

// TestBuild_DynamicType exercises the full conversion path (stroke data,
// SplitDistanceMetres, sportFor, watts) for a "dynamic" (dynamic rower)
// result - this machine type had never been run through Build with real or
// synthetic data before.
func TestBuild_DynamicType(t *testing.T) {
	detail := concept2.ResultDetail{
		Result: concept2.Result{
			ID:            5,
			Date:          "2026-09-18 12:00:00",
			DateUTC:       "2026-09-18 12:00:00",
			Distance:      2000,
			Time:          4800,
			CaloriesTotal: 200,
			Type:          "dynamic",
			HeartRate:     concept2.HeartRate{Average: 150},
		},
	}
	detail.Strokes.Data = []concept2.Stroke{
		{Time: 2400, Distance: 10000, Pace: 1200, StrokeRate: 22, HeartRate: 145},
		{Time: 4800, Distance: 20000, Pace: 1200, StrokeRate: 22, HeartRate: 155},
	}

	body, err := Build(detail)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	var doc trainingCenterDatabase
	if err := xml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("output is not well-formed XML: %v", err)
	}

	if got := doc.Activities.Activity.Sport; got != "Other" {
		t.Errorf("Sport = %q, want Other", got)
	}
	if got := len(doc.Activities.Activity.Laps); got != 1 {
		t.Fatalf("len(Laps) = %d, want 1", got)
	}
	lap := doc.Activities.Activity.Laps[0]
	if lap.DistanceMeters != 2000 {
		t.Errorf("DistanceMeters = %v, want 2000", lap.DistanceMeters)
	}
	// A dynamic rower uses the same 500m split as RowErg/SkiErg (not
	// BikeErg's 1000m), so watts should come out the same as the
	// WattsFromPace(1200) case tested above (203W), not the bike formula.
	for _, tp := range lap.Track.Trackpoint {
		if tp.Extensions == nil || tp.Extensions.TPX == nil {
			continue
		}
		if got := tp.Extensions.TPX.Watts; got != 203 {
			t.Errorf("trackpoint watts = %d, want 203", got)
		}
	}
}

// TestBuild_DynamicType_SegmentsOnly exercises the no-stroke-data fallback
// (lapsFromSegments) for a "dynamic" result with only split summaries.
func TestBuild_DynamicType_SegmentsOnly(t *testing.T) {
	detail := concept2.ResultDetail{
		Result: concept2.Result{
			ID:            6,
			Date:          "2026-09-18 12:00:00",
			DateUTC:       "2026-09-18 12:00:00",
			Distance:      2000,
			Time:          4800,
			CaloriesTotal: 200,
			Type:          "dynamic",
			Workout: concept2.Workout{
				Splits: []concept2.WorkoutSegment{
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
