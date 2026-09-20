FROM golang:1.24-alpine AS build
WORKDIR /src
COPY . .
RUN go test ./... && CGO_ENABLED=0 go build -ldflags="-s -w" -o /runtime/app/edge-gateway ./cmd/gateway \
    && mkdir -p /runtime/app/data \
    && cp -R web /runtime/app/web \
    && chown -R 10001:10001 /runtime/app
FROM scratch
WORKDIR /app
COPY --from=build --chown=10001:10001 /runtime/app /app
USER 10001:10001
EXPOSE 18083
CMD ["/app/edge-gateway"]
