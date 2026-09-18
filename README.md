# concept2garmin

A small Go CLI that talks to the [Concept2 Logbook API](https://log.concept2.com/developers/documentation/)
to list your recent ergometer workouts and download one at a time as a
Garmin-compatible `.tcx` file (heart rate, cadence, and — for
rower/SkiErg/dynamic pieces with stroke data — estimated watts).

Uploading to Strava is included as a stretch feature, using Strava's own
upload API and OAuth flow.

## Requirements

- Go 1.22+
- A Concept2 Logbook API access token (see their
  [developer docs](https://log.concept2.com/developers/documentation/) for
  how to obtain one)

## Build

```bash
make build        # builds ./dist/concept2garmin
```

## Usage

Save your token once — it's cached at `~/.config/concept2garmin/concept2.token`
(owner-only permissions) so you don't have to pass `--token` or set
`CONCEPT2_TOKEN` again:

```bash
./dist/concept2garmin auth your-access-token
```

`--token`/`CONCEPT2_TOKEN` still work and take priority when set (and are
themselves cached to that file for next time), so a one-off
`--token ...` on any command also works without running `auth` first.

List your 10 most recent workouts, numbered 1 (most recent) upward:

```bash
./dist/concept2garmin list
```

```
#   Date             Type     Distance        Time  Workout
1   2026-09-18 12:31 bike      13079m     30:00.0  VariableInterval
2   2026-09-07 06:56 bike        742m      2:23.8  JustRow
...
```

Show metadata for a single workout (by the position shown above) — date,
type, distance, duration, calories, heart rate, etc.:

```bash
./dist/concept2garmin show 1
```

Download a single workout (by the position shown above) as a `.tcx` file
into `./workout/`:

```bash
./dist/concept2garmin get 1
```

`get` only ever downloads one workout per invocation. It reuses the exact
result shown by your last `list` call when possible (via
`workout/.last_list.json`), so `get 3` really is the workout you saw at
position 3.

Import the resulting `.tcx` file into Garmin Connect via
**Import Data** on the Garmin Connect website, or drag-and-drop it onto
Strava's **Upload Activity** page.

### Stretch: uploading straight to Strava

1. Create a Strava API application at <https://www.strava.com/settings/api>
   and note its Client ID and Client Secret.
2. Authorize once (opens your browser; you log in and grant access
   yourself — this tool never sees your Strava password):
   ```bash
   export STRAVA_CLIENT_ID=...
   export STRAVA_CLIENT_SECRET=...
   ./dist/concept2garmin strava-auth
   ```
3. Upload everything in `./workout/` that hasn't been uploaded yet:
   ```bash
   ./dist/concept2garmin strava-upload
   ```

## How watts are computed

Concept2's published power formula, `watts = 2.80 / (split_seconds / 500)^3`,
is applied to every machine type. "Split" is whatever Concept2 itself uses
as the pace value: time per 500m for RowErg/SkiErg/dynamic, time per 1000m
for BikeErg — the same `/500` divisor is used either way, since Concept2
doesn't rescale BikeErg's split before applying the formula (see the
[Concept2 watts calculator](https://www.concept2.com/training/watts-calculator)
and [Erg Arcade's pace derivatives writeup](https://ergarcade.com/articles/c2-pace-derivatives)).

Workouts with stroke-by-stroke data (`stroke_data: true` in the API) get a
per-stroke watts value. Workouts without it fall back to one trackpoint per
interval/split, with watts estimated from each segment's own average pace.

## Development

```bash
make fmt-check vet build test
```
