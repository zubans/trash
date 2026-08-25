FROM golang:1.26-alpine AS build
WORKDIR /app

# Copy module files from backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download

# Copy backend source files, including nested packages
COPY backend/ .

# Build the main binary
RUN go build -o /app/healthlogin .

# Build the release registration tool
RUN go build -o /app/release ./cmd/release

# Build the standalone migration runner
RUN go build -o /app/migrate ./cmd/migrate

# Runtime image
FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=build /app/healthlogin .
COPY --from=build /app/release .
COPY --from=build /app/migrate .
# Migrations ship with the image: the server applies pending ones on start.
COPY --from=build /app/migrations ./migrations
RUN mkdir -p /app/certs
EXPOSE 8080
CMD ["./healthlogin"]
