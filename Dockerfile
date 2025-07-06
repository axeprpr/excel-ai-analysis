FROM golang:1.24-bookworm AS builder

WORKDIR /src

COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/excel-ai-analysis ./cmd/server

FROM debian:bookworm-slim

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl sqlite3 \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /out/excel-ai-analysis /usr/local/bin/excel-ai-analysis

ENV ADDR=:8080
ENV DATA_DIR=/app/data

RUN mkdir -p /app/data

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD ["curl", "-fsS", "http://127.0.0.1:8080/readyz"]

CMD ["excel-ai-analysis"]
