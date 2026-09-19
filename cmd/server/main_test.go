package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	fbauth "firebase.google.com/go/v4/auth"

	"github.com/eoinaokane/concept2upload/internal/strava"
	"github.com/eoinaokane/concept2upload/internal/webstore"
)

// fakeVerifier is a test double for idTokenVerifier: idToken values are
// looked up directly against a map, standing in for what a real Firebase
// project would otherwise need to verify a JWT.
type fakeVerifier struct {
	uidByToken map[string]string
}

func (f *fakeVerifier) VerifyIDToken(ctx context.Context, idToken string) (*fbauth.Token, error) {
	uid, ok := f.uidByToken[idToken]
	if !ok {
		return nil, errors.New("invalid token")
	}
	return &fbauth.Token{UID: uid}, nil
}

// fakeStore is an in-memory test double for tokenStore, standing in for
// what a real Firestore project would otherwise need.
type fakeStore struct {
	concept2Tokens map[string]string
	stravaTokens   map[string]strava.Token
	oauthStates    map[string]string // state -> uid
	uploads        map[string]webstore.UploadRecord

	saveConcept2TokenErr error
	getUploadErr         error
	consumeStateErr      error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		concept2Tokens: map[string]string{},
		stravaTokens:   map[string]strava.Token{},
		oauthStates:    map[string]string{},
		uploads:        map[string]webstore.UploadRecord{},
	}
}

func uploadKey(uid string, resultID int64) string { return fmt.Sprintf("%s:%d", uid, resultID) }

func (f *fakeStore) GetConcept2Token(ctx context.Context, uid string) (string, error) {
	tok, ok := f.concept2Tokens[uid]
	if !ok {
		return "", webstore.ErrNotFound
	}
	return tok, nil
}

func (f *fakeStore) SaveConcept2Token(ctx context.Context, uid, token string) error {
	if f.saveConcept2TokenErr != nil {
		return f.saveConcept2TokenErr
	}
	f.concept2Tokens[uid] = token
	return nil
}

func (f *fakeStore) GetStravaToken(ctx context.Context, uid string) (strava.Token, error) {
	tok, ok := f.stravaTokens[uid]
	if !ok {
		return strava.Token{}, webstore.ErrNotFound
	}
	return tok, nil
}

func (f *fakeStore) SaveStravaToken(ctx context.Context, uid string, tok strava.Token) error {
	f.stravaTokens[uid] = tok
	return nil
}

func (f *fakeStore) SaveOAuthState(ctx context.Context, state, uid string) error {
	f.oauthStates[state] = uid
	return nil
}

func (f *fakeStore) ConsumeOAuthState(ctx context.Context, state string) (string, error) {
	if f.consumeStateErr != nil {
		return "", f.consumeStateErr
	}
	uid, ok := f.oauthStates[state]
	if !ok {
		return "", webstore.ErrNotFound
	}
	delete(f.oauthStates, state)
	return uid, nil
}

func (f *fakeStore) GetUpload(ctx context.Context, uid string, resultID int64) (webstore.UploadRecord, error) {
	if f.getUploadErr != nil {
		return webstore.UploadRecord{}, f.getUploadErr
	}
	rec, ok := f.uploads[uploadKey(uid, resultID)]
	if !ok {
		return webstore.UploadRecord{}, webstore.ErrNotFound
	}
	return rec, nil
}

func (f *fakeStore) SaveUpload(ctx context.Context, uid string, resultID, activityID int64) error {
	f.uploads[uploadKey(uid, resultID)] = webstore.UploadRecord{StravaActivityID: activityID}
	return nil
}

func withUID(r *http.Request, uid string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), uidKey, uid))
}

// --- auth middleware ---------------------------------------------------

func TestWithAuth(t *testing.T) {
	srv := &server{auth: &fakeVerifier{uidByToken: map[string]string{"good-token": "uid-1"}}}

	var gotUID string
	handler := srv.withAuth(func(w http.ResponseWriter, r *http.Request) {
		gotUID = uidFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	t.Run("missing header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer bad-token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("valid token", func(t *testing.T) {
		gotUID = ""
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer good-token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
		if gotUID != "uid-1" {
			t.Errorf("uid reaching the handler = %q, want uid-1", gotUID)
		}
	})
}

// --- handleSaveConcept2Token ---------------------------------------------

func TestHandleSaveConcept2Token(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		store := newFakeStore()
		srv := &server{store: store}

		req := withUID(httptest.NewRequest(http.MethodPost, "/api/concept2-token", strings.NewReader(`{"token":"c2-token"}`)), "uid-1")
		rec := httptest.NewRecorder()
		srv.handleSaveConcept2Token(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body)
		}
		if store.concept2Tokens["uid-1"] != "c2-token" {
			t.Errorf("stored token = %q, want c2-token", store.concept2Tokens["uid-1"])
		}
	})

	t.Run("empty token", func(t *testing.T) {
		srv := &server{store: newFakeStore()}
		req := withUID(httptest.NewRequest(http.MethodPost, "/api/concept2-token", strings.NewReader(`{}`)), "uid-1")
		rec := httptest.NewRecorder()
		srv.handleSaveConcept2Token(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		srv := &server{store: newFakeStore()}
		req := withUID(httptest.NewRequest(http.MethodPost, "/api/concept2-token", strings.NewReader(`not json`)), "uid-1")
		rec := httptest.NewRecorder()
		srv.handleSaveConcept2Token(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}

// --- handleStravaAuthorizeURL ---------------------------------------------

func TestHandleStravaAuthorizeURL(t *testing.T) {
	t.Run("missing server config", func(t *testing.T) {
		srv := &server{store: newFakeStore()}
		req := withUID(httptest.NewRequest(http.MethodGet, "/api/strava/authorize-url", nil), "uid-1")
		rec := httptest.NewRecorder()
		srv.handleStravaAuthorizeURL(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rec.Code)
		}
	})

	t.Run("success", func(t *testing.T) {
		store := newFakeStore()
		srv := &server{store: store, stravaClientID: "client-123", publicBaseURL: "https://example.com"}
		req := withUID(httptest.NewRequest(http.MethodGet, "/api/strava/authorize-url", nil), "uid-1")
		rec := httptest.NewRecorder()
		srv.handleStravaAuthorizeURL(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body)
		}
		var resp struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decoding response: %v", err)
		}
		if !strings.Contains(resp.URL, "client_id=client-123") {
			t.Errorf("url = %q, missing client_id", resp.URL)
		}
		if !strings.Contains(resp.URL, "example.com%2Fapi%2Fstrava%2Fcallback") {
			t.Errorf("url = %q, missing redirect_uri to this deployment's callback", resp.URL)
		}
		if len(store.oauthStates) != 1 {
			t.Fatalf("oauthStates = %v, want exactly one saved state", store.oauthStates)
		}
		for _, uid := range store.oauthStates {
			if uid != "uid-1" {
				t.Errorf("saved state's uid = %q, want uid-1", uid)
			}
		}
	})
}

// --- handleStravaCallback (error paths only - success needs real Strava) --

func TestHandleStravaCallback_Errors(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"missing code and state", "/api/strava/callback"},
		{"denied", "/api/strava/callback?error=access_denied"},
		{"unknown state", "/api/strava/callback?code=abc&state=unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := &server{store: newFakeStore()}
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			rec := httptest.NewRecorder()
			srv.handleStravaCallback(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body = %s", rec.Code, rec.Body)
			}
		})
	}
}

// --- handleUploadStrava (already-uploaded short-circuit; needs no network)

func TestHandleUploadStrava_AlreadyUploaded(t *testing.T) {
	store := newFakeStore()
	store.uploads[uploadKey("uid-1", 42)] = webstore.UploadRecord{StravaActivityID: 999}
	srv := &server{store: store}

	req := withUID(httptest.NewRequest(http.MethodPost, "/api/workouts/42/upload-strava", nil), "uid-1")
	req.SetPathValue("id", "42")
	rec := httptest.NewRecorder()
	srv.handleUploadStrava(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body)
	}
	var resp struct {
		StravaActivityID int64 `json:"stravaActivityId"`
		AlreadyUploaded  bool  `json:"alreadyUploaded"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.StravaActivityID != 999 || !resp.AlreadyUploaded {
		t.Errorf("response = %+v, want activity 999 already uploaded", resp)
	}
}

// --- small pure helpers ----------------------------------------------------

func TestPathID(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
		want    int64
	}{
		{"valid", "42", false, 42},
		{"zero", "0", true, 0},
		{"negative", "-1", true, 0},
		{"not a number", "abc", true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.SetPathValue("id", tc.value)
			got, err := pathID(req)
			if (err != nil) != tc.wantErr {
				t.Fatalf("pathID(%q) error = %v, wantErr %v", tc.value, err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("pathID(%q) = %d, want %d", tc.value, got, tc.want)
			}
		})
	}
}

func TestActivityTypeFromConcept2Type(t *testing.T) {
	cases := map[string]string{
		"bike":    "ride",
		"rower":   "rowing",
		"dynamic": "rowing",
		"skierg":  "workout",
		"":        "workout",
	}
	for c2Type, want := range cases {
		if got := activityTypeFromConcept2Type(c2Type); got != want {
			t.Errorf("activityTypeFromConcept2Type(%q) = %q, want %q", c2Type, got, want)
		}
	}
}

func TestRandomState(t *testing.T) {
	a, err := randomState()
	if err != nil {
		t.Fatalf("randomState: %v", err)
	}
	b, err := randomState()
	if err != nil {
		t.Fatalf("randomState: %v", err)
	}
	if a == b {
		t.Error("randomState() returned the same value on consecutive calls")
	}
	if len(a) != 32 { // 16 random bytes, hex-encoded
		t.Errorf("len(randomState()) = %d, want 32", len(a))
	}
}
