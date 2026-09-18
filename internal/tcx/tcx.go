// Package tcx converts a Concept2 result into a Garmin Training Center
// Database (TCX) document, suitable for importing into Garmin Connect or
// uploading to Strava.
package tcx

import (
	"encoding/xml"
	"fmt"
	"math"
	"time"

	"github.com/eoinaokane/concept2garmin/internal/concept2"
)

const (
	tcxNamespace = "http://www.garmin.com/xmlschemas/TrainingCenterDatabase/v2"
	tpxNamespace = "http://www.garmin.com/xmlschemas/ActivityExtension/v2"

	// SourceURL is credited in every exported file's <Notes> (and in the
	// Strava upload description), so an activity retains a pointer back to
	// how it was produced.
	SourceURL = "https://github.com/eoinaokane/concept2garmin"
)

// buildNotes combines the workout's own Concept2 comment (if any) with a
// short attribution back to SourceURL.
func buildNotes(concept2Comment string) string {
	attribution := "Exported from Concept2 via " + SourceURL
	if concept2Comment == "" {
		return attribution
	}
	return concept2Comment + "\n\n" + attribution
}

// point is an internal, unit-normalized representation of a single sample
// before it's rendered into the TCX XML structs.
type point struct {
	Time           time.Time
	DistanceMeters float64
	HeartRateBpm   int
	Cadence        int
	Watts          int
}

// lapData is one lap's worth of points plus the lap-level totals Concept2
// reports for it (its own time/distance/calories, not running totals).
type lapData struct {
	TimeTenths     int
	DistanceMeters float64
	Calories       int
	Points         []point
}

// Build renders a Concept2 result as a TCX document, with one <Lap> per
// Concept2 interval/split so Garmin Connect and Strava show them as
// separate segments rather than one lap covering the whole workout.
func Build(detail concept2.ResultDetail) ([]byte, error) {
	start, err := detail.StartTime()
	if err != nil {
		return nil, fmt.Errorf("tcx: could not determine start time for result %d: %w", detail.ID, err)
	}

	laps := buildLaps(detail, start)
	if len(laps) == 0 {
		// No intervals/splits and no stroke data at all; synthesize a
		// single start/end lap covering the whole result.
		laps = []lapData{{
			TimeTenths:     detail.Time,
			DistanceMeters: float64(detail.Distance),
			Calories:       detail.CaloriesTotal,
			Points: []point{
				{Time: start, DistanceMeters: 0, HeartRateBpm: detail.HeartRate.Average},
				{
					Time:           start.Add(time.Duration(detail.Time) * 100 * time.Millisecond),
					DistanceMeters: float64(detail.Distance),
					HeartRateBpm:   detail.HeartRate.Average,
					Cadence:        clampByte(detail.StrokeRate),
				},
			},
		}}
	}

	xmlLaps := make([]lap, 0, len(laps))
	cursor := start
	for _, l := range laps {
		xmlLaps = append(xmlLaps, renderLap(cursor, l))
		cursor = cursor.Add(time.Duration(l.TimeTenths) * 100 * time.Millisecond)
	}

	doc := trainingCenterDatabase{
		XMLNSDefault: tcxNamespace,
		XMLNSXSI:     "http://www.w3.org/2001/XMLSchema-instance",
		XMLNSNS3:     tpxNamespace,
		SchemaLocation: tcxNamespace +
			" https://www8.garmin.com/xmlschemas/TrainingCenterDatabasev2.xsd",
		Activities: activities{
			Activity: activity{
				Sport: sportFor(detail.Type),
				ID:    start.UTC().Format(time.RFC3339),
				Laps:  xmlLaps,
				Notes: buildNotes(detail.Comments),
			},
		},
	}

	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("tcx: marshalling result %d failed: %w", detail.ID, err)
	}
	return append([]byte(xml.Header), body...), nil
}

// buildLaps prefers stroke-by-stroke data when available (grouped into laps
// at each interval boundary), falling back to one lap per interval/split
// summary, and finally no laps at all (Build then synthesizes one lap
// covering the whole result).
func buildLaps(detail concept2.ResultDetail, start time.Time) []lapData {
	segments := detail.Workout.Intervals
	if len(segments) == 0 {
		segments = detail.Workout.Splits
	}

	if len(detail.Strokes.Data) > 0 {
		laps := lapsFromStrokes(detail.Strokes.Data, start)
		// Concept2's own interval/split totals are more precise than what
		// we can derive from stroke samples (which are only reported to
		// the nearest tenth of a second/decimetre), so prefer them when
		// the counts line up.
		if len(segments) == len(laps) {
			for i := range laps {
				laps[i].TimeTenths = segments[i].Time
				laps[i].DistanceMeters = float64(segments[i].Distance)
				laps[i].Calories = segments[i].CaloriesTotal
			}
		}
		return laps
	}
	if len(segments) > 0 {
		return lapsFromSegments(segments, start, SplitDistanceMetres(detail.Type))
	}
	return nil
}

// lapsFromStrokes converts stroke-level samples (time in tenths of a
// second, distance in decimetres, both cumulative *within the current
// interval*) into one lap per interval, with absolute whole-workout
// timestamps and distances.
func lapsFromStrokes(strokes []concept2.Stroke, start time.Time) []lapData {
	var laps []lapData
	var curPoints []point
	var timeOffsetTenths, distOffsetDeci, lastT, lastD int
	lapStartTenths, lapStartDeci := 0, 0

	flush := func() {
		if len(curPoints) == 0 {
			return
		}
		endTenths := timeOffsetTenths + lastT
		endDeci := distOffsetDeci + lastD
		laps = append(laps, lapData{
			TimeTenths:     endTenths - lapStartTenths,
			DistanceMeters: float64(endDeci-lapStartDeci) / 10.0,
			Points:         curPoints,
		})
		curPoints = nil
		lapStartTenths, lapStartDeci = endTenths, endDeci
	}

	for i, s := range strokes {
		if i > 0 && s.Time < lastT {
			// A new interval started; carry the previous interval's totals
			// forward so time/distance keep climbing across the workout,
			// and close out the lap we were building.
			timeOffsetTenths += lastT
			distOffsetDeci += lastD
			flush()
		}
		absTenths := timeOffsetTenths + s.Time
		absDeci := distOffsetDeci + s.Distance

		curPoints = append(curPoints, point{
			Time:           start.Add(time.Duration(absTenths) * 100 * time.Millisecond),
			DistanceMeters: float64(absDeci) / 10.0,
			HeartRateBpm:   s.HeartRate,
			Cadence:        clampByte(s.StrokeRate),
			Watts:          WattsFromPace(s.Pace),
		})
		lastT, lastD = s.Time, s.Distance
	}
	flush()
	return laps
}

// segmentSampleTenths is how often (tenths of a second) lapsFromSegments
// emits a trackpoint within a lap. Concept2 only gives us one average pace
// per segment (no per-second data), so every sample in a lap repeats that
// segment's constant watts/cadence/heart rate. Sampling every second
// rather than just emitting a start/end point matters: analysis tools
// (Strava, TrainingPeaks, etc.) that compute time-in-zone from trackpoints
// treat the gaps between samples as near-zero power, so two sparse points
// per lap made a real ~230W effort look like 99% Zone 1.
const segmentSampleTenths = 10

// lapsFromSegments builds one lap per interval/split, densely sampled
// every segmentSampleTenths, using each segment's own (non-cumulative)
// time and distance to advance a running total across laps. This is
// coarser than stroke data (a constant estimate per segment rather than a
// true per-stroke value) but is all the API returns for workouts recorded
// without per-stroke logging.
func lapsFromSegments(segments []concept2.WorkoutSegment, start time.Time, splitDistanceMetres int) []lapData {
	laps := make([]lapData, 0, len(segments))
	var cumTenths, cumDist int
	for _, seg := range segments {
		segStartTenths, segStartDist := cumTenths, cumDist
		cumTenths += seg.Time
		cumDist += seg.Distance

		watts := WattsFromDistanceTime(seg.Distance, seg.Time, splitDistanceMetres)
		cadence := clampByte(seg.StrokeRate)
		hr := seg.HeartRate.Ending

		var points []point
		for t := 0; t < seg.Time; t += segmentSampleTenths {
			frac := float64(t) / float64(seg.Time)
			points = append(points, point{
				Time:           start.Add(time.Duration(segStartTenths+t) * 100 * time.Millisecond),
				DistanceMeters: float64(segStartDist) + frac*float64(seg.Distance),
				HeartRateBpm:   hr,
				Cadence:        cadence,
				Watts:          watts,
			})
		}
		points = append(points, point{
			Time:           start.Add(time.Duration(cumTenths) * 100 * time.Millisecond),
			DistanceMeters: float64(cumDist),
			HeartRateBpm:   hr,
			Cadence:        cadence,
			Watts:          watts,
		})

		laps = append(laps, lapData{
			TimeTenths:     seg.Time,
			DistanceMeters: float64(seg.Distance),
			Calories:       seg.CaloriesTotal,
			Points:         points,
		})
	}
	return laps
}

// renderLap turns a lapData into the TCX <Lap> element, including
// Garmin-style Average/MaximumHeartRateBpm and average Cadence summaries
// derived from that lap's own trackpoints.
func renderLap(startTime time.Time, l lapData) lap {
	avgHR, maxHR := heartRateStats(l.Points)
	xmlLap := lap{
		StartTime:        startTime.UTC().Format(time.RFC3339),
		TotalTimeSeconds: float64(l.TimeTenths) / 10.0,
		DistanceMeters:   l.DistanceMeters,
		Calories:         l.Calories,
		Intensity:        "Active",
		Cadence:          avgCadence(l.Points),
		TriggerMethod:    "Manual",
		Track:            track{Trackpoint: renderTrackpoints(l.Points)},
	}
	if avgHR > 0 {
		xmlLap.AverageHeartRateBpm = &heartRateBpm{Value: avgHR}
	}
	if maxHR > 0 {
		xmlLap.MaximumHeartRateBpm = &heartRateBpm{Value: maxHR}
	}
	return xmlLap
}

func heartRateStats(points []point) (avg, max int) {
	var sum, count int
	for _, p := range points {
		if p.HeartRateBpm <= 0 {
			continue
		}
		sum += p.HeartRateBpm
		count++
		if p.HeartRateBpm > max {
			max = p.HeartRateBpm
		}
	}
	if count == 0 {
		return 0, 0
	}
	return sum / count, max
}

func avgCadence(points []point) int {
	var sum, count int
	for _, p := range points {
		if p.Cadence <= 0 {
			continue
		}
		sum += p.Cadence
		count++
	}
	if count == 0 {
		return 0
	}
	return sum / count
}

// SplitDistanceMetres returns the reference distance Concept2 uses for a
// machine's displayed "split"/pace: 500m for RowErg/SkiErg/dynamic, 1000m
// for BikeErg. See WattsFromPace.
func SplitDistanceMetres(c2Type string) int {
	if c2Type == "bike" {
		return 1000
	}
	return 500
}

// WattsFromDistanceTime derives a Concept2-style split (tenths of a second
// per splitDistanceMetres) from a distance (metres) covered over a
// duration (tenths of a second), then applies WattsFromPace. Useful for
// estimating average power over an entire result or segment when only
// distance/time totals are known (rather than stroke-by-stroke pace
// samples). splitDistanceMetres must match the machine (see
// SplitDistanceMetres) since BikeErg's split is defined per 1000m rather
// than the 500m used by RowErg/SkiErg.
func WattsFromDistanceTime(distanceMetres, timeTenths, splitDistanceMetres int) int {
	if distanceMetres <= 0 || timeTenths <= 0 || splitDistanceMetres <= 0 {
		return 0
	}
	paceTenthsPerSplit := int(math.Round(float64(timeTenths) * float64(splitDistanceMetres) / float64(distanceMetres)))
	return WattsFromPace(paceTenthsPerSplit)
}

// WattsFromPace applies Concept2's published power formula,
// watts = 2.80 / (split/500)^3, where split is the pace value in seconds
// as Concept2 itself defines and displays it: time per 500m for
// RowErg/SkiErg, time per 1000m for BikeErg. The same formula and the same
// "/500" divisor apply to both - Concept2 does not rescale BikeErg's split
// before using it - so this works unchanged across machine types.
// See https://www.concept2.com/training/watts-calculator and
// https://ergarcade.com/articles/c2-pace-derivatives.
func WattsFromPace(paceTenthsPer500 int) int {
	if paceTenthsPer500 <= 0 {
		return 0
	}
	paceSeconds := float64(paceTenthsPer500) / 10.0
	watts := 2.80 / math.Pow(paceSeconds/500.0, 3)
	return int(math.Round(watts))
}

func clampByte(v int) int {
	if v < 0 {
		return 0
	}
	if v > 254 {
		return 254
	}
	return v
}

// sportFor maps a Concept2 machine type onto the three sports the TCX
// schema allows (Running, Biking, Other). Rowing and SkiErg both land on
// "Other"; the Strava/Garmin importer still keeps HR, cadence and watts,
// and the activity type can be relabeled after import.
func sportFor(c2Type string) string {
	if c2Type == "bike" {
		return "Biking"
	}
	return "Other"
}

func renderTrackpoints(points []point) []trackpoint {
	out := make([]trackpoint, 0, len(points))
	for _, p := range points {
		tp := trackpoint{
			Time:           p.Time.UTC().Format(time.RFC3339),
			DistanceMeters: p.DistanceMeters,
		}
		if p.HeartRateBpm > 0 {
			tp.HeartRateBpm = &heartRateBpm{Value: p.HeartRateBpm}
		}
		if p.Cadence > 0 {
			tp.Cadence = p.Cadence
		}
		if p.Watts > 0 {
			tp.Extensions = &extensions{TPX: &tpx{Watts: p.Watts}}
		}
		out = append(out, tp)
	}
	return out
}

// --- TCX XML structs -------------------------------------------------------

type trainingCenterDatabase struct {
	XMLName        xml.Name   `xml:"TrainingCenterDatabase"`
	XMLNSDefault   string     `xml:"xmlns,attr"`
	XMLNSXSI       string     `xml:"xmlns:xsi,attr"`
	XMLNSNS3       string     `xml:"xmlns:ns3,attr"`
	SchemaLocation string     `xml:"xsi:schemaLocation,attr"`
	Activities     activities `xml:"Activities"`
}

type activities struct {
	Activity activity `xml:"Activity"`
}

type activity struct {
	Sport string `xml:"Sport,attr"`
	ID    string `xml:"Id"`
	Laps  []lap  `xml:"Lap"`
	Notes string `xml:"Notes,omitempty"`
}

// lap field order follows the TCX schema's required sequence for
// ActivityLap_t (StartTime, TotalTimeSeconds, DistanceMeters, Calories,
// Average/MaximumHeartRateBpm, Intensity, Cadence, TriggerMethod, Track) -
// Garmin Connect and other strict TCX readers reject an out-of-order file.
type lap struct {
	StartTime           string        `xml:"StartTime,attr"`
	TotalTimeSeconds    float64       `xml:"TotalTimeSeconds"`
	DistanceMeters      float64       `xml:"DistanceMeters"`
	Calories            int           `xml:"Calories"`
	AverageHeartRateBpm *heartRateBpm `xml:"AverageHeartRateBpm,omitempty"`
	MaximumHeartRateBpm *heartRateBpm `xml:"MaximumHeartRateBpm,omitempty"`
	Intensity           string        `xml:"Intensity"`
	Cadence             int           `xml:"Cadence,omitempty"`
	TriggerMethod       string        `xml:"TriggerMethod"`
	Track               track         `xml:"Track"`
}

type track struct {
	Trackpoint []trackpoint `xml:"Trackpoint"`
}

type trackpoint struct {
	Time           string        `xml:"Time"`
	DistanceMeters float64       `xml:"DistanceMeters"`
	HeartRateBpm   *heartRateBpm `xml:"HeartRateBpm,omitempty"`
	Cadence        int           `xml:"Cadence,omitempty"`
	Extensions     *extensions   `xml:"Extensions,omitempty"`
}

type heartRateBpm struct {
	Value int `xml:"Value"`
}

type extensions struct {
	TPX *tpx `xml:"ns3:TPX,omitempty"`
}

type tpx struct {
	Watts int `xml:"ns3:Watts,omitempty"`
}
