FROM golang:1.26-alpine AS build
WORKDIR /app
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ .
RUN go build -o /app/opsbot ./cmd/opsbot

FROM alpine:3.21
# make and the Docker CLI are what `make restart` needs; docker-cli-compose
# supplies the `docker compose` subcommand the Makefile calls.
RUN apk --no-cache add ca-certificates make docker-cli docker-cli-compose
COPY --from=build /app/opsbot /usr/local/bin/opsbot
# No EXPOSE: the bot polls Telegram outbound and listens on nothing.
ENTRYPOINT ["/usr/local/bin/opsbot"]
