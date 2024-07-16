# Build stage.
FROM golang:1.22-alpine as builder
WORKDIR /go/src/m-lab/autojoin
COPY . .
RUN go mod download

RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /go/bin/register ./cmd/register

# Run stage.
FROM alpine:3.20
RUN apk add curl
COPY --from=builder /go/bin/register .
ENTRYPOINT ["/register"]
