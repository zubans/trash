FROM golang:1.26-alpine AS build
WORKDIR /app

# Copy module files from backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download

# Copy backend source files, including nested packages
COPY backend/ .

# Build the binary
RUN go build -o /app/healthlogin .

# Runtime image
FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=build /app/healthlogin .
EXPOSE 8080
CMD ["./healthlogin"]
