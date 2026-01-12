# Build stage.
FROM golang:1.25-alpine as builder
RUN apk add --no-cache git
WORKDIR /go/src/m-lab/autojoin
COPY . .
RUN go mod download
RUN GIT_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "0.0.0") && \
    CGO_ENABLED=0 go build -ldflags="-s -w -X main.Version=${GIT_TAG}" -o /go/bin/register ./cmd/register

# Run stage.
FROM alpine:3.20
RUN apk add curl
COPY --from=builder /go/bin/register .
ENTRYPOINT ["/register"]
