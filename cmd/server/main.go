// Command server is the multi-user web app counterpart to
// cmd/concept2upload: it exposes the same list/get/upload-strava
// operations as a small JSON API, backed by Firebase Auth (who's asking)
// and Firestore (each user's Concept2/Strava tokens), so it can serve many
// people at once instead of one local CLI user.
//
// It's designed to run on Cloud Run behind Firebase Hosting (see
// firebase.json and Dockerfile at the repo root): Hosting terminates the
// public domain and forwards /api/** here, Cloud Run's default service
// account gives this process credentials for Firebase/Firestore with no
// key file needed, and the frontend in web/ talks to this API directly.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	firebase "firebase.google.com/go/v4"
	fbauth "firebase.google.com/go/v4/auth"

	"github.com/eoinaokane/concept2upload/internal/concept2"
	"github.com/eoinaokane/concept2upload/internal/strava"
	"github.com/eoinaokane/concept2upload/internal/tcx"
	"github.com/eoinaokane/concept2upload/internal/webstore"
)

func main() {
	ctx := context.Background()

	app, err := firebase.NewApp(ctx, nil)
	if err != nil {
		log.Fatalf("initializing Firebase app: %v", err)
	}
	authClient, err := app.Auth(ctx)
	if err != nil {
		log.Fatalf("initializing Firebase Auth client: %v", err)
	}
	fsClient, err := app.Firestore(ctx)
	if err != nil {
		log.Fatalf("initializing Firestore client: %v", err)
	}
	defer fsClient.Close()

	srv := &server{
		auth:               authClient,
		store:              webstore.New(fsClient),
		stravaClientID:     os.Getenv("STRAVA_CLIENT_ID"),
		stravaClientSecret: os.Getenv("STRAVA_CLIENT_SECRET"),
		publicBaseURL:      strings.TrimSuffix(os.Getenv("PUBLIC_BASE_URL"), "/"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/strava/callback", srv.handleStravaCallback) // public: Strava redirects the browser here directly

	mux.Handle("POST /api/concept2-token", srv.withAuth(srv.handleSaveConcept2Token))
	mux.Handle("GET /api/workouts", srv.withAuth(srv.handleListWorkouts))
	mux.Handle("GET /api/workouts/{id}", srv.withAuth(srv.handleGetWorkout))
	mux.Handle("GET /api/workouts/{id}/tcx", srv.withAuth(srv.handleGetWorkoutTCX))
	mux.Handle("GET /api/strava/authorize-url", srv.withAuth(srv.handleStravaAuthorizeURL))
	mux.Handle("POST /api/workouts/{id}/upload-strava", srv.withAuth(srv.handleUploadStrava))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("listening on :%s", port)
	if err := http.ListenAndServe(":"+port, withCORS(mux)); err != nil {
		log.Fatal(err)
	}
}

type server struct {
	auth  *fbauth.Client
	store *webstore.Store

	// stravaClientID/stravaClientSecret are this deployment's own Strava
	// API application credentials (from https://www.strava.com/settings/api)
	// - one app shared by every user, unlike the per-user OAuth tokens it
	// hands out, which live in Firestore. Set via env vars on Cloud Run.
	stravaClientID     string
	stravaClientSecret string

	// publicBaseURL is this app's own public URL (e.g.
	// https://yourapp.web.app), used to build the Strava OAuth redirect_uri.
	publicBaseURL string
}

// --- auth middleware ---------------------------------------------------

type ctxKey int

const uidKey ctxKey = 0

// withAuth requires a Firebase Auth ID token in the Authorization header
// (as sent by the Firebase JS SDK after sign-in), verifies it, and makes
// the resulting UID available to the handler via uidFromContext.
func (s *server) withAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		idToken, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || idToken == "" {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		token, err := s.auth.VerifyIDToken(r.Context(), idToken)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		ctx := context.WithValue(r.Context(), uidKey, token.UID)
		next(w, r.WithContext(ctx))
	})
}

func uidFromContext(ctx context.Context) string {
	uid, _ := ctx.Value(uidKey).(string)
	return uid
}

// withCORS allows the frontend (served from Firebase Hosting, possibly on
// a different origin during local development) to call this API directly.
// In production behind Firebase Hosting rewrites, frontend and API share
// one origin and this is a no-op.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- helpers -------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// concept2Client resolves uid's saved Concept2 token and returns a client,
// or a user-facing error if they haven't run "save token" yet.
func (s *server) concept2Client(r *http.Request) (*concept2.Client, error) {
	uid := uidFromContext(r.Context())
	token, err := s.store.GetConcept2Token(r.Context(), uid)
	if errors.Is(err, webstore.ErrNotFound) {
		return nil, fmt.Errorf("no Concept2 token saved; POST /api/concept2-token first")
	}
	if err != nil {
		return nil, err
	}
	return concept2.NewClient(token), nil
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid workout id")
	}
	return id, nil
}

// --- handlers --------------------------------------------------------------

func (s *server) handleSaveConcept2Token(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Token) == "" {
		writeError(w, http.StatusBadRequest, "expected JSON body {\"token\": \"...\"}")
		return
	}
	uid := uidFromContext(r.Context())
	if err := s.store.SaveConcept2Token(r.Context(), uid, strings.TrimSpace(body.Token)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// workoutSummary is the JSON shape for one row of "list" - unlike the
// CLI's positional numbering (backed by a .last_list.json cache file),
// the web API just returns each workout's real Concept2 result ID, which
// the frontend keeps client-side and uses directly in later requests.
type workoutSummary struct {
	ID            int64  `json:"id"`
	Date          string `json:"date"`
	Type          string `json:"type"`
	Distance      int    `json:"distanceMetres"`
	TimeFormatted string `json:"timeFormatted"`
	WorkoutType   string `json:"workoutType"`
}

func (s *server) handleListWorkouts(w http.ResponseWriter, r *http.Request) {
	client, err := s.concept2Client(r)
	if err != nil {
		writeError(w, http.StatusPreconditionFailed, err.Error())
		return
	}
	limit := 10
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	results, err := client.ListLatest(limit)
	if err != nil {
		writeError(w, http.StatusBadGateway, "listing Concept2 workouts: "+err.Error())
		return
	}
	if len(results) > limit {
		results = results[:limit]
	}

	summaries := make([]workoutSummary, 0, len(results))
	for _, res := range results {
		dateStr := res.Date
		if start, err := res.StartTime(); err == nil {
			dateStr = start.Format(time.RFC3339)
		}
		summaries = append(summaries, workoutSummary{
			ID:            res.ID,
			Date:          dateStr,
			Type:          res.Type,
			Distance:      res.Distance,
			TimeFormatted: res.TimeFormatted,
			WorkoutType:   res.WorkoutType,
		})
	}
	writeJSON(w, http.StatusOK, summaries)
}

// workoutDetail is the JSON shape of "show", covering the same fields the
// CLI's printWorkoutMetadata prints.
type workoutDetail struct {
	workoutSummary
	Calories      int    `json:"calories"`
	DragFactor    int    `json:"dragFactor,omitempty"`
	StrokeRate    int    `json:"strokeRate,omitempty"`
	AvgWatts      int    `json:"avgWatts,omitempty"`
	HeartRateAvg  int    `json:"heartRateAvg,omitempty"`
	HeartRateMax  int    `json:"heartRateMax,omitempty"`
	Source        string `json:"source,omitempty"`
	HasStrokeData bool   `json:"hasStrokeData"`
	Comments      string `json:"comments,omitempty"`
}

func (s *server) fetchDetail(r *http.Request) (concept2.ResultDetail, error) {
	client, err := s.concept2Client(r)
	if err != nil {
		return concept2.ResultDetail{}, err
	}
	id, err := pathID(r)
	if err != nil {
		return concept2.ResultDetail{}, err
	}
	return client.GetResultDetail(id)
}

func (s *server) handleGetWorkout(w http.ResponseWriter, r *http.Request) {
	detail, err := s.fetchDetail(r)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	dateStr := detail.Date
	if start, err := detail.StartTime(); err == nil {
		dateStr = start.Format(time.RFC3339)
	}
	watts := tcx.WattsFromDistanceTime(detail.Distance, detail.Time, tcx.SplitDistanceMetres(detail.Type))

	writeJSON(w, http.StatusOK, workoutDetail{
		workoutSummary: workoutSummary{
			ID:            detail.ID,
			Date:          dateStr,
			Type:          detail.Type,
			Distance:      detail.Distance,
			TimeFormatted: detail.TimeFormatted,
			WorkoutType:   detail.WorkoutType,
		},
		Calories:      detail.CaloriesTotal,
		DragFactor:    detail.DragFactor,
		StrokeRate:    detail.StrokeRate,
		AvgWatts:      watts,
		HeartRateAvg:  detail.HeartRate.Average,
		HeartRateMax:  detail.HeartRate.Max,
		Source:        detail.Source,
		HasStrokeData: detail.StrokeData,
		Comments:      detail.Comments,
	})
}

func (s *server) handleGetWorkoutTCX(w http.ResponseWriter, r *http.Request) {
	detail, err := s.fetchDetail(r)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	body, err := tcx.Build(detail)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "building TCX: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/vnd.garmin.tcx+xml")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="workout-%d.tcx"`, detail.ID))
	_, _ = w.Write(body)
}

// handleStravaAuthorizeURL starts a Strava OAuth flow for the signed-in
// user: it records a one-time state value against their UID (so the
// public callback below knows whose token to save) and hands the frontend
// the URL to redirect the browser to.
func (s *server) handleStravaAuthorizeURL(w http.ResponseWriter, r *http.Request) {
	if s.stravaClientID == "" || s.publicBaseURL == "" {
		writeError(w, http.StatusInternalServerError, "server missing STRAVA_CLIENT_ID/PUBLIC_BASE_URL configuration")
		return
	}
	state, err := randomState()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	uid := uidFromContext(r.Context())
	if err := s.store.SaveOAuthState(r.Context(), state, uid); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	redirectURI := s.publicBaseURL + "/api/strava/callback"
	writeJSON(w, http.StatusOK, map[string]string{
		"url": strava.BuildAuthorizeURL(s.stravaClientID, redirectURI, state),
	})
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// handleStravaCallback is the public endpoint Strava's OAuth consent
// screen redirects the user's browser back to - unlike every other
// handler here, it's not behind withAuth (Strava can't send our bearer
// token), so the state value is what ties this request back to a
// specific signed-in user (see handleStravaAuthorizeURL).
func (s *server) handleStravaCallback(w http.ResponseWriter, r *http.Request) {
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		http.Error(w, "Strava authorization denied: "+errParam, http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		http.Error(w, "missing code/state", http.StatusBadRequest)
		return
	}

	uid, err := s.store.ConsumeOAuthState(r.Context(), state)
	if errors.Is(err, webstore.ErrNotFound) {
		http.Error(w, "authorization link expired or already used; try again", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tok, err := strava.ExchangeCode(s.stravaClientID, s.stravaClientSecret, code)
	if err != nil {
		http.Error(w, "exchanging Strava code: "+err.Error(), http.StatusBadGateway)
		return
	}
	if err := s.store.SaveStravaToken(r.Context(), uid, tok); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fmt.Fprint(w, "Strava connected. You can close this tab and return to the app.")
}

// stravaAccessToken returns a valid Strava access token for uid, silently
// refreshing it first if it's expired (mirrors strava.AccessToken, but
// against Firestore instead of a local token file).
func (s *server) stravaAccessToken(ctx context.Context, uid string) (string, error) {
	tok, err := s.store.GetStravaToken(ctx, uid)
	if errors.Is(err, webstore.ErrNotFound) {
		return "", fmt.Errorf("Strava not connected; GET /api/strava/authorize-url first")
	}
	if err != nil {
		return "", err
	}
	if time.Now().Unix() < tok.ExpiresAt-60 {
		return tok.AccessToken, nil
	}
	refreshed, err := strava.RefreshAccessToken(s.stravaClientID, s.stravaClientSecret, tok.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("refreshing Strava token: %w", err)
	}
	if err := s.store.SaveStravaToken(ctx, uid, refreshed); err != nil {
		return "", err
	}
	return refreshed.AccessToken, nil
}

func (s *server) handleUploadStrava(w http.ResponseWriter, r *http.Request) {
	uid := uidFromContext(r.Context())
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if existing, err := s.store.GetUpload(r.Context(), uid, id); err == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"stravaActivityId": existing.StravaActivityID,
			"alreadyUploaded":  true,
		})
		return
	}

	client, err := s.concept2Client(r)
	if err != nil {
		writeError(w, http.StatusPreconditionFailed, err.Error())
		return
	}
	detail, err := client.GetResultDetail(id)
	if err != nil {
		writeError(w, http.StatusBadGateway, "fetching workout detail: "+err.Error())
		return
	}
	body, err := tcx.Build(detail)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "building TCX: "+err.Error())
		return
	}

	accessToken, err := s.stravaAccessToken(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusPreconditionFailed, err.Error())
		return
	}

	tmp, err := os.CreateTemp("", fmt.Sprintf("concept2upload-%d-*.tcx", id))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tmp.Close()

	name := fmt.Sprintf("workout-%d", id)
	result, err := strava.UploadTCX(accessToken, tmp.Name(), name, "Uploaded via "+tcx.SourceURL, activityTypeFromConcept2Type(detail.Type))
	if err != nil {
		writeError(w, http.StatusBadGateway, "uploading to Strava: "+err.Error())
		return
	}

	if err := s.store.SaveUpload(r.Context(), uid, id, result.ActivityID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"stravaActivityId": result.ActivityID,
		"alreadyUploaded":  false,
	})
}

func activityTypeFromConcept2Type(c2Type string) string {
	switch c2Type {
	case "bike":
		return "ride"
	case "rower", "dynamic":
		return "rowing"
	default:
		return "workout"
	}
}
