# Builds cmd/server for Cloud Run. Firebase Hosting (see firebase.json)
# rewrites /api/** to this service; it never serves the frontend itself
# (that's web/, deployed straight to Hosting).
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/server /server
# Cloud Run sets $PORT and injects Application Default Credentials via the
# service's own identity - no key file needed for Firebase Admin/Firestore.
ENTRYPOINT ["/server"]
