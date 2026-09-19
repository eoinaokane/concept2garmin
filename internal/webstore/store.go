// Package webstore is the multi-user counterpart to internal/concept2's
// and internal/strava's local-file token storage: instead of one token
// cached on disk for whoever runs the CLI, it keeps one Firestore document
// per signed-in web app user, keyed by their Firebase Auth UID.
package webstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/eoinaokane/concept2upload/internal/strava"
)

// ErrNotFound is returned when a user has no saved value for what was
// asked for (e.g. hasn't run auth-concept2/auth-strava yet).
var ErrNotFound = errors.New("webstore: not found")

const (
	usersCollection       = "users"
	oauthStatesCollection = "oauthStates"
	uploadsSubcollection  = "uploads"

	// oauthStateTTL bounds how long a Strava "authorize" link stays valid
	// before it must be requested again, and how long a stray/abandoned
	// state document survives.
	oauthStateTTL = 10 * time.Minute
)

type Store struct {
	client *firestore.Client
}

func New(client *firestore.Client) *Store {
	return &Store{client: client}
}

// userDoc mirrors, per-user, what the CLI keeps in
// concept2.TokenPath()/strava.TokenPath(): the Concept2 API token and the
// Strava OAuth token pair.
type userDoc struct {
	Concept2Token      string `firestore:"concept2Token,omitempty"`
	StravaAccessToken  string `firestore:"stravaAccessToken,omitempty"`
	StravaRefreshToken string `firestore:"stravaRefreshToken,omitempty"`
	StravaExpiresAt    int64  `firestore:"stravaExpiresAt,omitempty"`
}

func (s *Store) userRef(uid string) *firestore.DocumentRef {
	return s.client.Collection(usersCollection).Doc(uid)
}

// GetConcept2Token returns uid's saved Concept2 API token, or ErrNotFound.
func (s *Store) GetConcept2Token(ctx context.Context, uid string) (string, error) {
	snap, err := s.userRef(uid).Get(ctx)
	if isNotFound(err) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	var d userDoc
	if err := snap.DataTo(&d); err != nil {
		return "", err
	}
	if d.Concept2Token == "" {
		return "", ErrNotFound
	}
	return d.Concept2Token, nil
}

// SaveConcept2Token saves uid's Concept2 API token, equivalent to the CLI's
// 'auth-concept2 <token>'.
func (s *Store) SaveConcept2Token(ctx context.Context, uid, token string) error {
	_, err := s.userRef(uid).Set(ctx, map[string]interface{}{
		"concept2Token": token,
	}, firestore.MergeAll)
	return err
}

// GetStravaToken returns uid's saved Strava OAuth token pair, or
// ErrNotFound if they haven't authorized Strava yet.
func (s *Store) GetStravaToken(ctx context.Context, uid string) (strava.Token, error) {
	snap, err := s.userRef(uid).Get(ctx)
	if isNotFound(err) {
		return strava.Token{}, ErrNotFound
	}
	if err != nil {
		return strava.Token{}, err
	}
	var d userDoc
	if err := snap.DataTo(&d); err != nil {
		return strava.Token{}, err
	}
	if d.StravaAccessToken == "" {
		return strava.Token{}, ErrNotFound
	}
	return strava.Token{
		AccessToken:  d.StravaAccessToken,
		RefreshToken: d.StravaRefreshToken,
		ExpiresAt:    d.StravaExpiresAt,
	}, nil
}

// SaveStravaToken saves uid's Strava OAuth token pair, equivalent to what
// the CLI's 'auth-strava' persists to strava.TokenPath().
func (s *Store) SaveStravaToken(ctx context.Context, uid string, tok strava.Token) error {
	_, err := s.userRef(uid).Set(ctx, map[string]interface{}{
		"stravaAccessToken":  tok.AccessToken,
		"stravaRefreshToken": tok.RefreshToken,
		"stravaExpiresAt":    tok.ExpiresAt,
	}, firestore.MergeAll)
	return err
}

// oauthStateDoc records which signed-in user started a Strava "authorize"
// redirect, so the callback - which Strava calls directly, with no way to
// attach our own Authorization header - can tell whose token to save. It
// also serves as the standard OAuth CSRF check: the callback only accepts a
// state value this server itself handed out.
type oauthStateDoc struct {
	UID       string    `firestore:"uid"`
	CreatedAt time.Time `firestore:"createdAt"`
}

// SaveOAuthState records that the random value state was issued to start a
// Strava authorization flow for uid.
func (s *Store) SaveOAuthState(ctx context.Context, state, uid string) error {
	_, err := s.client.Collection(oauthStatesCollection).Doc(state).Set(ctx, oauthStateDoc{
		UID:       uid,
		CreatedAt: time.Now(),
	})
	return err
}

// ConsumeOAuthState looks up which user a Strava callback's state value
// belongs to and deletes it (states are single-use), returning
// ErrNotFound if it's unknown or has expired.
func (s *Store) ConsumeOAuthState(ctx context.Context, state string) (uid string, err error) {
	ref := s.client.Collection(oauthStatesCollection).Doc(state)
	snap, err := ref.Get(ctx)
	if isNotFound(err) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	var d oauthStateDoc
	if err := snap.DataTo(&d); err != nil {
		return "", err
	}
	_, _ = ref.Delete(ctx)
	if time.Since(d.CreatedAt) > oauthStateTTL {
		return "", ErrNotFound
	}
	return d.UID, nil
}

// UploadRecord is what's remembered about a workout already uploaded to
// Strava, equivalent to one entry of the CLI's manifest.json.
type UploadRecord struct {
	StravaActivityID int64 `firestore:"stravaActivityId"`
}

func (s *Store) uploadRef(uid string, resultID int64) *firestore.DocumentRef {
	return s.userRef(uid).Collection(uploadsSubcollection).Doc(fmt.Sprintf("%d", resultID))
}

// GetUpload returns the Strava activity ID resultID was already uploaded
// as for uid, or ErrNotFound if it hasn't been uploaded yet.
func (s *Store) GetUpload(ctx context.Context, uid string, resultID int64) (UploadRecord, error) {
	snap, err := s.uploadRef(uid, resultID).Get(ctx)
	if isNotFound(err) {
		return UploadRecord{}, ErrNotFound
	}
	if err != nil {
		return UploadRecord{}, err
	}
	var r UploadRecord
	if err := snap.DataTo(&r); err != nil {
		return UploadRecord{}, err
	}
	return r, nil
}

// SaveUpload records that uid uploaded resultID to Strava as activityID.
func (s *Store) SaveUpload(ctx context.Context, uid string, resultID, activityID int64) error {
	_, err := s.uploadRef(uid, resultID).Set(ctx, UploadRecord{StravaActivityID: activityID})
	return err
}

// isNotFound reports whether err is what firestore.DocumentRef.Get returns
// for a document that doesn't exist (a gRPC status error with code
// codes.NotFound) - the client library has no plain sentinel for this.
func isNotFound(err error) bool {
	return status.Code(err) == codes.NotFound
}
