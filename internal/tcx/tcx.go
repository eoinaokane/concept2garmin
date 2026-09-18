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
)

// point is an internal, unit-normalized representation of a single sample
// before it's rendered into the TCX XML structs.
type point struct {
	Time           time.Time
	DistanceMeters float64
	HeartRateBpm   int
	Cadence        int
	Watts          int
}

// Build renders a Concept2 result as a TCX document.
func Build(detail concept2.ResultDetail) ([]byte, error) {
	start, err := detail.StartTime()
	if err != nil {
		return nil, fmt.Errorf("tcx: could not determine start time for result %d: %w", detail.ID, err)
	}

	points := buildPoints(detail, start)
	if len(points) < 2 {
		// Always have at least a start and end point so Strava/Garmin
		// accept the lap.
		points = []point{
			{Time: start, DistanceMeters: 0, HeartRateBpm: detail.HeartRate.Average},
			{
				Time:           start.Add(time.Duration(detail.Time) * 100 * time.Millisecond),
				DistanceMeters: float64(detail.Distance),
				HeartRateBpm:   detail.HeartRate.Average,
				Cadence:        detail.StrokeRate,
			},
		}
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
				Lap: lap{
					StartTime:        start.UTC().Format(time.RFC3339),
					TotalTimeSeconds: float64(detail.Time) / 10.0,
					DistanceMeters:   float64(detail.Distance),
					Calories:         detail.CaloriesTotal,
					Intensity:        "Active",
					TriggerMethod:    "Manual",
					Track:            track{Trackpoint: renderTrackpoints(points)},
				},
			},
		},
	}

	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("tcx: marshalling result %d failed: %w", detail.ID, err)
	}
	return append([]byte(xml.Header), body...), nil
}

// buildPoints prefers stroke-by-stroke data when available, falling back to
// the coarser interval/split summary, and finally to no intermediate points
// at all (Build then synthesizes a two-point lap).
func buildPoints(detail concept2.ResultDetail, start time.Time) []point {
	if len(detail.Strokes.Data) > 0 {
		return pointsFromStrokes(detail.Strokes.Data, detail.Type, start)
	}
	segments := detail.Workout.Intervals
	if len(segments) == 0 {
		segments = detail.Workout.Splits
	}
	if len(segments) > 0 {
		return pointsFromSegments(segments, start)
	}
	return nil
}

// pointsFromStrokes converts stroke-level samples (time in tenths of a
// second, distance in decimetres, both cumulative *within the current
// interval*) into absolute, whole-workout samples.
func pointsFromStrokes(strokes []concept2.Stroke, sport string, start time.Time) []point {
	points := make([]point, 0, len(strokes))
	var timeOffsetTenths, distOffsetDeci, lastT, lastD int
	for i, s := range strokes {
		if i > 0 && s.Time < lastT {
			// A new interval started; carry the previous interval's totals
			// forward so time/distance keep climbing across the workout.
			timeOffsetTenths += lastT
			distOffsetDeci += lastD
		}
		absTenths := timeOffsetTenths + s.Time
		absDeci := distOffsetDeci + s.Distance

		points = append(points, point{
			Time:           start.Add(time.Duration(absTenths) * 100 * time.Millisecond),
			DistanceMeters: float64(absDeci) / 10.0,
			HeartRateBpm:   s.HeartRate,
			Cadence:        clampByte(s.StrokeRate),
			Watts:          wattsFromPace(s.Pace, sport),
		})
		lastT, lastD = s.Time, s.Distance
	}
	return points
}

// pointsFromSegments builds one trackpoint per interval/split, using each
// segment's own (non-cumulative) time and distance to advance a running
// total. This is coarser than stroke data but is all the API returns for
// workouts recorded without per-stroke logging.
func pointsFromSegments(segments []concept2.WorkoutSegment, start time.Time) []point {
	points := make([]point, 0, len(segments)+1)
	points = append(points, point{Time: start, DistanceMeters: 0})

	var cumTenths, cumDist int
	for _, seg := range segments {
		cumTenths += seg.Time
		cumDist += seg.Distance
		points = append(points, point{
			Time:           start.Add(time.Duration(cumTenths) * 100 * time.Millisecond),
			DistanceMeters: float64(cumDist),
			HeartRateBpm:   seg.HeartRate.Ending,
			Cadence:        clampByte(seg.StrokeRate),
		})
	}
	return points
}

// wattsFromPace applies Concept2's published power formula,
// watts = 2.80 / (pace/500)^3 (pace in seconds per 500m; e.g. a 2:00/500m
// split is ~202W). It only applies to rower/skierg/dynamic ergs: BikeErg
// reports pace per 1000m over a different drag curve that this formula
// does not model, so bike results are left without a Watts value rather
// than showing a misleading number.
func wattsFromPace(paceTenthsPer500 int, sport string) int {
	if paceTenthsPer500 <= 0 || sport == "bike" {
		return 0
	}
	paceSecondsPer500 := float64(paceTenthsPer500) / 10.0
	watts := 2.80 / math.Pow(paceSecondsPer500/500.0, 3)
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
	Lap   lap    `xml:"Lap"`
}

type lap struct {
	StartTime        string  `xml:"StartTime,attr"`
	TotalTimeSeconds float64 `xml:"TotalTimeSeconds"`
	DistanceMeters   float64 `xml:"DistanceMeters"`
	Calories         int     `xml:"Calories"`
	Intensity        string  `xml:"Intensity"`
	TriggerMethod    string  `xml:"TriggerMethod"`
	Track            track   `xml:"Track"`
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
