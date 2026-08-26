# The prober needs two things that do not ship together: our Go binary and the
# xray core that actually speaks VLESS. Both are copied into one small image.
FROM golang:1.26-alpine AS build
WORKDIR /app
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ .
RUN go build -o /app/vlessprobe ./cmd/vlessprobe

# Pinned rather than :latest — a core that changes under the prober would turn
# an upstream regression into a false alert about our own servers.
FROM ghcr.io/xtls/xray-core:26.3.27 AS xray

FROM alpine:3.21
RUN apk --no-cache add ca-certificates
COPY --from=build /app/vlessprobe /usr/local/bin/vlessprobe
COPY --from=xray /usr/local/bin/xray /usr/local/bin/xray
# xray refuses to start without these when a config mentions geo rules. They are
# tiny and shipping them costs nothing.
COPY --from=xray /usr/local/share/xray/ /usr/local/share/xray/
EXPOSE 9102
ENTRYPOINT ["/usr/local/bin/vlessprobe"]
