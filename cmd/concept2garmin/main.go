// Command concept2garmin lists your Concept2 logbook workouts and
// downloads a chosen one as a Garmin-compatible TCX file (with heart rate,
// cadence, and watts where available). It can also, as a stretch feature,
// upload previously downloaded TCX files to Strava.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/eoinaokane/concept2garmin/internal/concept2"
	"github.com/eoinaokane/concept2garmin/internal/strava"
	"github.com/eoinaokane/concept2garmin/internal/tcx"
)

const defaultLimit = 10
const defaultDir = "workout"

// version follows Semantic Versioning (https://semver.org/): MAJOR.MINOR.PATCH,
// incremented for incompatible CLI/API changes, backwards-compatible
// features, and backwards-compatible fixes respectively. Overridden at
// release build time via -ldflags "-X main.version=..." (see
// .goreleaser.yaml), so it must stay a var, not a const.
var version = "0.2.3"

func main() {
	tokenFlag := &cli.StringFlag{
		Name:    "token",
		Sources: cli.EnvVars("CONCEPT2_TOKEN"),
		Usage:   "Concept2 Logbook API access token",
	}
	dirFlag := &cli.StringFlag{
		Name:  "dir",
		Value: defaultDir,
		Usage: "directory to save/read downloaded workouts",
	}

	cmd := &cli.Command{
		Name:    "concept2garmin",
		Usage:   "list and download Concept2 logbook workouts as Garmin-compatible TCX files",
		Version: version,
		Commands: []*cli.Command{
			{
				Name:      "auth",
				Usage:     "save your Concept2 API token locally so --token/CONCEPT2_TOKEN aren't needed every time",
				ArgsUsage: "<token>",
				Action:    runAuth,
			},
			{
				Name:      "list",
				Usage:     "show your most recent Concept2 workouts, numbered for use with 'get'",
				ArgsUsage: " ",
				Flags: []cli.Flag{
					tokenFlag,
					&cli.IntFlag{Name: "limit", Value: defaultLimit, Usage: "how many recent workouts to show"},
					dirFlag,
				},
				Action: runList,
			},
			{
				Name:      "show",
				Usage:     "show metadata for one workout (by the position shown in 'list')",
				ArgsUsage: "<position>",
				Flags: []cli.Flag{
					tokenFlag,
					&cli.IntFlag{Name: "limit", Value: defaultLimit, Usage: "how many recent workouts 'position' is counted against, if 'list' hasn't been run yet"},
					dirFlag,
				},
				Action: runShow,
			},
			{
				Name:      "get",
				Usage:     "download one workout (by the position shown in 'list') as a .tcx file",
				ArgsUsage: "<position>",
				Flags: []cli.Flag{
					tokenFlag,
					&cli.IntFlag{Name: "limit", Value: defaultLimit, Usage: "how many recent workouts 'position' is counted against, if 'list' hasn't been run yet"},
					dirFlag,
				},
				Action: runGet,
			},
			{
				Name:  "strava-auth",
				Usage: "(stretch) one-time OAuth authorization to allow uploads to your Strava account",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "client-id", Sources: cli.EnvVars("STRAVA_CLIENT_ID")},
					&cli.StringFlag{Name: "client-secret", Sources: cli.EnvVars("STRAVA_CLIENT_SECRET")},
				},
				Action: runStravaAuth,
			},
			{
				Name:  "strava-upload",
				Usage: "(stretch) upload previously downloaded .tcx files in --dir to Strava",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "client-id", Sources: cli.EnvVars("STRAVA_CLIENT_ID")},
					&cli.StringFlag{Name: "client-secret", Sources: cli.EnvVars("STRAVA_CLIENT_SECRET")},
					dirFlag,
				},
				Action: runStravaUpload,
			},
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func runAuth(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != 1 {
		return fmt.Errorf("expected exactly one argument: your Concept2 API token (e.g. 'concept2garmin auth abc123')")
	}
	if err := concept2.SaveToken(cmd.Args().First()); err != nil {
		return fmt.Errorf("saving token: %w", err)
	}
	path, _ := concept2.TokenPath()
	fmt.Printf("Concept2 token saved to %s\n", path)
	return nil
}

// resolveToken prefers an explicit --token flag (or CONCEPT2_TOKEN env var,
// which the flag is already sourced from), persisting it for next time, and
// otherwise falls back to a token saved earlier via 'concept2garmin auth'.
func resolveToken(cmd *cli.Command) (string, error) {
	if t := strings.TrimSpace(cmd.String("token")); t != "" {
		if err := concept2.SaveToken(t); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not cache token locally: %v\n", err)
		}
		return t, nil
	}
	if t, err := concept2.LoadStoredToken(); err == nil && t != "" {
		return t, nil
	}
	return "", fmt.Errorf("no Concept2 token found; run 'concept2garmin auth <token>' or pass --token/CONCEPT2_TOKEN")
}

// resolveStravaCredentials prefers explicit --client-id/--client-secret
// flags (or STRAVA_CLIENT_ID/STRAVA_CLIENT_SECRET env vars, which the flags
// are already sourced from), persisting them to strava.ConfigPath() for
// next time, and otherwise falls back to credentials saved by a previous
// run - so they only need to be supplied once.
func resolveStravaCredentials(cmd *cli.Command) (clientID, clientSecret string, err error) {
	id := strings.TrimSpace(cmd.String("client-id"))
	secret := strings.TrimSpace(cmd.String("client-secret"))
	if id != "" && secret != "" {
		if err := strava.SaveConfig(strava.Config{ClientID: id, ClientSecret: secret}); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not cache Strava credentials locally: %v\n", err)
		}
		return id, secret, nil
	}
	if c, err := strava.LoadConfig(); err == nil && c.ClientID != "" && c.ClientSecret != "" {
		return c.ClientID, c.ClientSecret, nil
	}
	return "", "", fmt.Errorf("no Strava API credentials found; pass --client-id/--client-secret (or STRAVA_CLIENT_ID/STRAVA_CLIENT_SECRET) once - see https://www.strava.com/settings/api")
}

// listCache remembers exactly which result ID was shown at each position by
// the last 'list' call, so a later 'get <position>' refers to the same
// workout even if new sessions have since been logged.
type listCache struct {
	FetchedAt time.Time            `json:"fetched_at"`
	Positions map[string]cacheItem `json:"positions"` // "1", "2", ...
}

type cacheItem struct {
	ResultID int64  `json:"result_id"`
	Summary  string `json:"summary"`
}

func cachePath(dir string) string { return filepath.Join(dir, ".last_list.json") }

func saveListCache(dir string, results []concept2.Result) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	c := listCache{FetchedAt: time.Now(), Positions: map[string]cacheItem{}}
	for i, r := range results {
		c.Positions[fmt.Sprintf("%d", i+1)] = cacheItem{ResultID: r.ID, Summary: summaryLine(r)}
	}
	body, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cachePath(dir), body, 0o644)
}

func loadListCache(dir string) (listCache, bool) {
	body, err := os.ReadFile(cachePath(dir))
	if err != nil {
		return listCache{}, false
	}
	var c listCache
	if err := json.Unmarshal(body, &c); err != nil {
		return listCache{}, false
	}
	return c, true
}

func summaryLine(r concept2.Result) string {
	start, err := r.StartTime()
	dateStr := r.Date
	if err == nil {
		dateStr = start.Format("2006-01-02 15:04")
	}
	return fmt.Sprintf("%-16s %-8s %6dm  %10s  %s", dateStr, r.Type, r.Distance, r.TimeFormatted, r.WorkoutType)
}

func runList(ctx context.Context, cmd *cli.Command) error {
	token, err := resolveToken(cmd)
	if err != nil {
		return err
	}
	limit := int(cmd.Int("limit"))
	dir := cmd.String("dir")

	client := concept2.NewClient(token)
	results, err := client.ListLatest(limit)
	if err != nil {
		return fmt.Errorf("listing Concept2 workouts: %w", err)
	}
	if len(results) > limit {
		results = results[:limit]
	}

	if err := saveListCache(dir, results); err != nil {
		return fmt.Errorf("saving list cache: %w", err)
	}

	fmt.Printf("%-3s %-16s %-8s %7s  %10s  %s\n", "#", "Date", "Type", "Distance", "Time", "Workout")
	for i, r := range results {
		fmt.Printf("%-3d %s\n", i+1, summaryLine(r))
	}
	fmt.Printf("\nRun 'concept2garmin get <#>' to download one as a .tcx file.\n")
	return nil
}

func runGet(ctx context.Context, cmd *cli.Command) error {
	token, err := resolveToken(cmd)
	if err != nil {
		return err
	}
	if cmd.Args().Len() != 1 {
		return fmt.Errorf("expected exactly one argument: the position from 'list' (e.g. 'concept2garmin get 1')")
	}
	position, err := parsePosition(cmd.Args().First())
	if err != nil {
		return err
	}
	dir := cmd.String("dir")
	limit := int(cmd.Int("limit"))

	client := concept2.NewClient(token)

	resultID, err := resolvePosition(client, dir, position, limit)
	if err != nil {
		return err
	}

	detail, err := client.GetResultDetail(resultID)
	if err != nil {
		return fmt.Errorf("fetching workout detail: %w", err)
	}

	body, err := tcx.Build(detail)
	if err != nil {
		return fmt.Errorf("building TCX: %w", err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	fullPath := uniqueFilePath(dir, sensibleFileName(detail))
	if err := os.WriteFile(fullPath, body, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", fullPath, err)
	}

	if err := recordDownload(dir, detail, filepath.Base(fullPath)); err != nil {
		return fmt.Errorf("updating manifest: %w", err)
	}

	fmt.Printf("saved %s\n", fullPath)
	return nil
}

func runShow(ctx context.Context, cmd *cli.Command) error {
	token, err := resolveToken(cmd)
	if err != nil {
		return err
	}
	if cmd.Args().Len() != 1 {
		return fmt.Errorf("expected exactly one argument: the position from 'list' (e.g. 'concept2garmin show 1')")
	}
	position, err := parsePosition(cmd.Args().First())
	if err != nil {
		return err
	}
	dir := cmd.String("dir")
	limit := int(cmd.Int("limit"))

	client := concept2.NewClient(token)
	resultID, err := resolvePosition(client, dir, position, limit)
	if err != nil {
		return err
	}

	detail, err := client.GetResultDetail(resultID)
	if err != nil {
		return fmt.Errorf("fetching workout detail: %w", err)
	}

	printWorkoutMetadata(position, detail)
	return nil
}

func printWorkoutMetadata(position int, d concept2.ResultDetail) {
	start, err := d.StartTime()
	dateStr := d.Date
	if err == nil {
		loc := ""
		if d.Timezone != "" {
			loc = " (" + d.Timezone + ")"
		}
		dateStr = start.Format("2006-01-02 15:04:05") + loc
	}

	fmt.Printf("Workout #%d (Concept2 id %d)\n", position, d.ID)
	printField("Date", dateStr)
	printField("Type", machineLabel(d.Type))
	printField("Workout type", valueOr(d.WorkoutType, "n/a"))
	printField("Distance", fmt.Sprintf("%d m", d.Distance))
	printField("Duration", d.TimeFormatted)
	printField("Calories", fmt.Sprintf("%d kcal", d.CaloriesTotal))
	printField("Drag factor", intOr(d.DragFactor, "n/a"))
	printField(cadenceLabel(d.Type), intOr(d.StrokeRate, "n/a"))
	printField("Avg power", avgPowerSummary(d))

	segments := d.Workout.Intervals
	segmentKind := "intervals"
	if len(segments) == 0 {
		segments = d.Workout.Splits
		segmentKind = "splits"
	}

	printField("Heart rate", heartRateSummary(d.HeartRate, segments))
	printField("Source", valueOr(d.Source, "n/a"))
	printField("Stroke-by-stroke data available", yesNo(d.StrokeData))

	if len(segments) > 0 {
		printField("Segments", fmt.Sprintf("%d %s", len(segments), segmentKind))
	}

	if d.Comments != "" {
		printField("Comments", d.Comments)
	}
}

// avgPowerSummary estimates average power over the whole result from its
// total distance and work time (Time excludes rest periods, so this is an
// average over time actually spent working, not elapsed session time).
// Applies to every machine type - see tcx.WattsFromPace for why the same
// formula covers BikeErg's per-1000m split too.
func avgPowerSummary(d concept2.ResultDetail) string {
	watts := tcx.WattsFromDistanceTime(d.Distance, d.Time, tcx.SplitDistanceMetres(d.Type))
	if watts <= 0 {
		return "n/a"
	}
	return fmt.Sprintf("%d W", watts)
}

func printField(label, value string) {
	fmt.Printf("%-34s %s\n", label+":", value)
}

// cadenceLabel picks the right term for a machine's revolution rate:
// rowers/SkiErgs report strokes per minute, but BikeErg reports pedal
// cadence in RPM, not a "stroke rate".
func cadenceLabel(c2Type string) string {
	if c2Type == "bike" {
		return "Avg cadence (rpm)"
	}
	return "Avg stroke rate (spm)"
}

// heartRateSummary prefers the API's whole-workout heart rate summary.
// Concept2 sometimes omits that (notably for interval workouts) while still
// returning an "ending" heart rate per interval/split, so this falls back
// to deriving avg/min/max from those instead of reporting "n/a" when heart
// rate data does exist, just not as a top-level summary.
func heartRateSummary(hr concept2.HeartRate, segments []concept2.WorkoutSegment) string {
	if hr.Average > 0 || hr.Min > 0 || hr.Max > 0 || hr.Ending > 0 {
		parts := []string{}
		if hr.Average > 0 {
			parts = append(parts, fmt.Sprintf("avg %d", hr.Average))
		}
		if hr.Min > 0 {
			parts = append(parts, fmt.Sprintf("min %d", hr.Min))
		}
		if hr.Max > 0 {
			parts = append(parts, fmt.Sprintf("max %d", hr.Max))
		}
		if hr.Ending > 0 {
			parts = append(parts, fmt.Sprintf("ending %d", hr.Ending))
		}
		return strings.Join(parts, ", ") + " bpm"
	}

	var sum, min, max, count int
	for _, seg := range segments {
		e := seg.HeartRate.Ending
		if e <= 0 {
			continue
		}
		if min == 0 || e < min {
			min = e
		}
		if e > max {
			max = e
		}
		sum += e
		count++
	}
	if count == 0 {
		return "n/a"
	}
	return fmt.Sprintf("avg %d, min %d, max %d bpm", sum/count, min, max)
}

func valueOr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

func intOr(v int, fallback string) string {
	if v == 0 {
		return fallback
	}
	return fmt.Sprintf("%d", v)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func parsePosition(arg string) (int, error) {
	var position int
	if _, err := fmt.Sscanf(arg, "%d", &position); err != nil || position < 1 {
		return 0, fmt.Errorf("position must be a positive number, got %q", arg)
	}
	return position, nil
}

// resolvePosition prefers the exact result ID recorded by the last 'list'
// run in --dir/.last_list.json. If there's no cache (or the position isn't
// in it), it falls back to fetching the latest workouts itself, using
// --limit (or the position, if larger) as the fetch size.
func resolvePosition(client *concept2.Client, dir string, position, limit int) (int64, error) {
	if c, ok := loadListCache(dir); ok {
		if item, ok := c.Positions[fmt.Sprintf("%d", position)]; ok {
			return item.ResultID, nil
		}
	}

	fetchLimit := limit
	if position > fetchLimit {
		fetchLimit = position
	}
	results, err := client.ListLatest(fetchLimit)
	if err != nil {
		return 0, fmt.Errorf("listing Concept2 workouts: %w", err)
	}
	if position > len(results) {
		return 0, fmt.Errorf("position %d out of range: only %d workout(s) found", position, len(results))
	}
	return results[position-1].ID, nil
}

// sensibleFileName produces a human-readable name like
// "2026-09-18-Bike-30min-13.1km.tcx" rather than exposing the raw result ID.
// The duration is derived from TimeFormatted (total elapsed time, the same
// value 'list' shows), not the raw Time field, which for interval workouts
// excludes rest periods and would otherwise disagree with 'list'.
func sensibleFileName(detail concept2.ResultDetail) string {
	start, err := detail.StartTime()
	datePart := "unknown-date"
	if err == nil {
		datePart = start.Format("2006-01-02-1504")
	}
	minutes := elapsedMinutes(detail.TimeFormatted, detail.Time)
	km := float64(detail.Distance) / 1000.0
	return fmt.Sprintf("%s-%s-%.0fmin-%.1fkm.tcx", datePart, machineLabel(detail.Type), minutes, km)
}

// uniqueFilePath returns dir/fileName if that path doesn't exist yet, or
// dir/fileName-N.tcx (incrementing N) otherwise, so 'get' never silently
// overwrites an existing download.
func uniqueFilePath(dir, fileName string) string {
	candidate := filepath.Join(dir, fileName)
	ext := filepath.Ext(fileName)
	base := strings.TrimSuffix(fileName, ext)
	for n := 1; ; n++ {
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
		candidate = filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, n, ext))
	}
}

// elapsedMinutes parses a Concept2 "M:SS.s" or "H:MM:SS.s" TimeFormatted
// string into minutes, falling back to the raw tenths-of-a-second Time
// field if TimeFormatted can't be parsed.
func elapsedMinutes(formatted string, rawTenths int) float64 {
	parts := strings.Split(formatted, ":")
	if len(parts) >= 2 {
		var hours, minutes float64
		seconds, errSec := parseFloatSafe(parts[len(parts)-1])
		minutes, errMin := parseFloatSafe(parts[len(parts)-2])
		if len(parts) == 3 {
			hours, _ = parseFloatSafe(parts[0])
		}
		if errSec == nil && errMin == nil {
			return hours*60 + minutes + seconds/60.0
		}
	}
	return float64(rawTenths) / 10.0 / 60.0
}

func parseFloatSafe(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}

func machineLabel(c2Type string) string {
	switch c2Type {
	case "bike":
		return "Bike"
	case "rower":
		return "Row"
	case "skierg":
		return "SkiErg"
	case "dynamic":
		return "DynamicRow"
	default:
		if c2Type == "" {
			return "Workout"
		}
		return strings.ToUpper(c2Type[:1]) + c2Type[1:]
	}
}

// --- manifest, shared with the stretch Strava upload commands --------------

type manifest struct {
	Entries map[string]manifestEntry `json:"entries"`
}

type manifestEntry struct {
	File             string `json:"file"`
	StravaActivityID int64  `json:"strava_activity_id,omitempty"`
	UploadedToStrava bool   `json:"uploaded_to_strava,omitempty"`
}

func manifestPath(dir string) string { return filepath.Join(dir, "manifest.json") }

func loadManifest(dir string) manifest {
	m := manifest{Entries: map[string]manifestEntry{}}
	body, err := os.ReadFile(manifestPath(dir))
	if err != nil {
		return m
	}
	_ = json.Unmarshal(body, &m)
	if m.Entries == nil {
		m.Entries = map[string]manifestEntry{}
	}
	return m
}

func saveManifest(dir string, m manifest) error {
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(manifestPath(dir), body, 0o644)
}

func recordDownload(dir string, detail concept2.ResultDetail, fileName string) error {
	m := loadManifest(dir)
	key := fmt.Sprintf("%d", detail.ID)
	entry := m.Entries[key]
	entry.File = fileName
	m.Entries[key] = entry
	return saveManifest(dir, m)
}

func runStravaAuth(ctx context.Context, cmd *cli.Command) error {
	clientID, clientSecret, err := resolveStravaCredentials(cmd)
	if err != nil {
		return err
	}
	if err := strava.Authorize(ctx, clientID, clientSecret); err != nil {
		return err
	}
	path, _ := strava.TokenPath()
	fmt.Printf("Strava authorized. Token saved to %s\n", path)
	return nil
}

func runStravaUpload(ctx context.Context, cmd *cli.Command) error {
	dir := cmd.String("dir")
	clientID, clientSecret, err := resolveStravaCredentials(cmd)
	if err != nil {
		return err
	}

	m := loadManifest(dir)
	accessToken, err := strava.AccessToken(clientID, clientSecret)
	if err != nil {
		return fmt.Errorf("run 'concept2garmin strava-auth' first: %w", err)
	}

	// Deterministic order so re-runs behave predictably.
	keys := make([]string, 0, len(m.Entries))
	for k := range m.Entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	uploaded := 0
	for _, key := range keys {
		entry := m.Entries[key]
		if entry.UploadedToStrava || entry.File == "" {
			continue
		}
		fullPath := filepath.Join(dir, entry.File)

		fmt.Printf("uploading %s...\n", entry.File)
		result, err := strava.UploadTCX(accessToken, fullPath, strings.TrimSuffix(entry.File, ".tcx"), "Uploaded via "+tcx.SourceURL, activityTypeFromFileName(entry.File))
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to upload %s: %v\n", entry.File, err)
			continue
		}

		entry.UploadedToStrava = true
		entry.StravaActivityID = result.ActivityID
		m.Entries[key] = entry
		uploaded++
		fmt.Printf("  -> Strava activity %d\n", result.ActivityID)
	}

	if err := saveManifest(dir, m); err != nil {
		return err
	}
	fmt.Printf("done: %d activit(y/ies) uploaded to Strava\n", uploaded)
	return nil
}

func activityTypeFromFileName(name string) string {
	switch {
	case strings.Contains(name, "-Bike-"):
		return "ride"
	case strings.Contains(name, "-Row-"), strings.Contains(name, "-DynamicRow-"):
		return "rowing"
	case strings.Contains(name, "-SkiErg-"):
		return "workout"
	default:
		return "workout"
	}
}
