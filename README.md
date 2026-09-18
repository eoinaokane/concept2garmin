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

Set your token once so you don't have to pass `--token` every time:

```bash
export CONCEPT2_TOKEN=your-access-token
```

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

Concept2's published power formula, `watts = 2.80 / (pace_per_500m_seconds / 500)^3`,
is applied to rower/SkiErg/dynamic pieces that have stroke-by-stroke data
(`stroke_data: true` in the API). BikeErg uses a different drag curve that
this formula doesn't model, so bike workouts are exported without a watts
value rather than a misleading one. Workouts without stroke-by-stroke data
fall back to one trackpoint per interval/split, without watts.

## Development

```bash
make fmt-check vet build test
```
