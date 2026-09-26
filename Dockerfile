FROM golang:1.27 AS builder

WORKDIR /app

COPY go.* ./

RUN go mod download

COPY . ./

RUN go build -v -o main

FROM debian:bookworm-slim

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates

RUN update-ca-certificates

WORKDIR /app

COPY --from=builder /app/main .
COPY --from=builder /app/config.toml .

EXPOSE 8090

ENTRYPOINT ["/app/main"]
