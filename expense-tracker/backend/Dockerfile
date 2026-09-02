# syntax=docker/dockerfile:1

# --- Stage 1: build the static server binary -------------------------------
# Builder matches the go directive in go.mod (go 1.25.0) so no toolchain
# download is needed at build time.
FROM golang:1.25 AS build
WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO off + static build so the binary runs on distroless/static.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /server ./cmd/server

# --- Stage 2: minimal runtime ----------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /server /server

# PORT (set by Cloud Run) and FUNCTION_TARGET (set per service by the module)
# are supplied at runtime — do not hardcode them here.
EXPOSE 8080
ENTRYPOINT ["/server"]
