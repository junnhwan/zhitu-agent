# ---- Build stage ----
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git ca-certificates

ENV GOPROXY=https://goproxy.cn,direct

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /app/zhitu-agent ./cmd/server

# ---- Runtime stage ----
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/zhitu-agent .
COPY --from=builder /app/config.yaml .
COPY --from=builder /app/system-prompt ./system-prompt/
COPY --from=builder /app/static ./static/

ENV GIN_MODE=release

EXPOSE 10010

ENTRYPOINT ["./zhitu-agent"]
