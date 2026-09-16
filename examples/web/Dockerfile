FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY examples/go.mod examples/go.sum ./examples/
RUN cd examples && go mod download
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build GOMAXPROCS=2 CGO_ENABLED=1 go -C examples build -p=1 -tags=nomsgpack -trimpath -ldflags="-s -w" -o /playground ./web

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /playground /app/playground
COPY examples/web/static /app/static
USER 65532:65532
ENV APP_ADDR=0.0.0.0:8080 APP_ASSETS=/app/static GIN_MODE=release
EXPOSE 8080
ENTRYPOINT ["/app/playground"]
