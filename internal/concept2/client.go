// Package concept2 talks to the Concept2 Logbook API
// (https://log.concept2.com/developers/documentation/) to list a user's
// workout results and fetch stroke-level detail for a single result.
package concept2

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"
)

const baseURL = "https://log.concept2.com"

// Client is a small wrapper around the Concept2 Logbook API using a
// bearer access token.
type Client struct {
	Token      string
	HTTPClient *http.Client
}

// NewClient returns a Client using the given access token.
func NewClient(token string) *Client {
	return &Client{
		Token:      token,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) get(path string, query url.Values, out interface{}) error {
	u := baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("concept2: request to %s failed: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("concept2: reading response from %s failed: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("concept2: %s returned %s: %s", path, resp.Status, string(body))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("concept2: decoding response from %s failed: %w", path, err)
	}
	return nil
}

// Pagination mirrors the "meta.pagination" block returned by the API.
type Pagination struct {
	Total       int `json:"total"`
	Count       int `json:"count"`
	PerPage     int `json:"per_page"`
	CurrentPage int `json:"current_page"`
	TotalPages  int `json:"total_pages"`
}

type resultsResponse struct {
	Data []Result `json:"data"`
	Meta struct {
		Pagination Pagination `json:"pagination"`
	} `json:"meta"`
}

// HeartRate holds heart rate summary/interval values as returned inline in
// results and splits/intervals.
type HeartRate struct {
	Average int `json:"average,omitempty"`
	Min     int `json:"min,omitempty"`
	Max     int `json:"max,omitempty"`
	Ending  int `json:"ending,omitempty"`
	Rest    int `json:"rest,omitempty"`
}

// WorkoutSegment is one entry of a result's workout.intervals or
// workout.splits array.
type WorkoutSegment struct {
	Type          string    `json:"type"`
	Time          int       `json:"time"`     // tenths of a second
	Distance      int       `json:"distance"` // metres
	CaloriesTotal int       `json:"calories_total"`
	StrokeRate    int       `json:"stroke_rate"`
	HeartRate     HeartRate `json:"heart_rate"`
}

// Workout holds the optional structured interval/split breakdown of a result.
type Workout struct {
	Intervals []WorkoutSegment `json:"intervals,omitempty"`
	Splits    []WorkoutSegment `json:"splits,omitempty"`
}

// Result is a single logbook entry as returned by
// GET /api/users/{user}/results (summary form, no stroke data).
type Result struct {
	ID            int64     `json:"id"`
	UserID        int64     `json:"user_id"`
	Date          string    `json:"date"` // "2026-09-18 12:31:00", local to Timezone
	Timezone      string    `json:"timezone"`
	DateUTC       string    `json:"date_utc"`
	Distance      int       `json:"distance"` // metres
	Type          string    `json:"type"`     // rower, bike, skierg, dynamic, ...
	Time          int       `json:"time"`     // tenths of a second
	TimeFormatted string    `json:"time_formatted"`
	WorkoutType   string    `json:"workout_type"`
	Source        string    `json:"source"`
	Comments      string    `json:"comments"`
	StrokeData    bool      `json:"stroke_data"`
	CaloriesTotal int       `json:"calories_total"`
	DragFactor    int       `json:"drag_factor"`
	StrokeRate    int       `json:"stroke_rate"`
	Workout       Workout   `json:"workout"`
	HeartRate     HeartRate `json:"heart_rate"`
}

// StartTime parses Date using Timezone, falling back to UTC parsing of
// DateUTC if the local timezone can't be loaded.
func (r Result) StartTime() (time.Time, error) {
	const layout = "2006-01-02 15:04:05"
	if r.Timezone != "" {
		if loc, err := time.LoadLocation(r.Timezone); err == nil {
			if t, err := time.ParseInLocation(layout, r.Date, loc); err == nil {
				return t, nil
			}
		}
	}
	return time.Parse(layout, r.DateUTC+"Z0700")
}

// ListResults fetches every result between from and to (inclusive),
// following pagination automatically. Concept2 caps page size at 250.
func (c *Client) ListResults(from, to time.Time) ([]Result, error) {
	var all []Result
	page := 1
	for {
		q := url.Values{}
		q.Set("from", from.Format("2006-01-02"))
		q.Set("to", to.Format("2006-01-02"))
		q.Set("number", "250")
		q.Set("page", strconv.Itoa(page))

		var resp resultsResponse
		if err := c.get("/api/users/me/results", q, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Data...)

		if page >= resp.Meta.Pagination.TotalPages || resp.Meta.Pagination.TotalPages == 0 {
			break
		}
		page++
	}
	return all, nil
}

// ListLatest fetches the most recent limit results, newest first, in a
// single request. from/to are intentionally omitted so the API returns
// across the user's whole history rather than requiring a date range.
func (c *Client) ListLatest(limit int) ([]Result, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 250 {
		limit = 250
	}
	q := url.Values{}
	q.Set("number", strconv.Itoa(limit))

	var resp resultsResponse
	if err := c.get("/api/users/me/results", q, &resp); err != nil {
		return nil, err
	}

	results := resp.Data
	sort.SliceStable(results, func(i, j int) bool {
		ti, errI := results[i].StartTime()
		tj, errJ := results[j].StartTime()
		if errI != nil || errJ != nil {
			return false
		}
		return ti.After(tj)
	})
	return results, nil
}

// Stroke is one entry of the stroke-by-stroke data returned when a result
// is fetched with include=strokes. Time and distance are cumulative within
// the current interval (they reset to zero at the start of each interval
// for interval workouts).
type Stroke struct {
	Time       int `json:"t"`   // tenths of a second, cumulative
	Distance   int `json:"d"`   // decimetres, cumulative
	Pace       int `json:"p"`   // tenths of a second per 500m (1000m for bike)
	StrokeRate int `json:"spm"` // strokes per minute
	HeartRate  int `json:"hr"`  // bpm, 0 if no HR source
}

// ResultDetail is a single result fetched with ?include=strokes.
type ResultDetail struct {
	Result
	Strokes struct {
		Data []Stroke `json:"data"`
	} `json:"strokes"`
}

type resultDetailResponse struct {
	Data ResultDetail `json:"data"`
}

// GetResultDetail fetches a single result, including stroke-by-stroke data
// when the underlying workout recorded it (Result.StrokeData).
func (c *Client) GetResultDetail(id int64) (ResultDetail, error) {
	q := url.Values{}
	q.Set("include", "strokes")
	var resp resultDetailResponse
	if err := c.get(fmt.Sprintf("/api/users/me/results/%d", id), q, &resp); err != nil {
		return ResultDetail{}, err
	}
	return resp.Data, nil
}
