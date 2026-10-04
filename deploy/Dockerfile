# syntax=docker/dockerfile:1

# --- build ---
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/permwatch ./cmd/permwatch

# --- run ---
# distroless/static: no shell, no package manager, CA certificates included.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/permwatch /permwatch
USER nonroot:nonroot
ENV PERMWATCH_STATE_DIR=/data
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/permwatch"]
CMD ["watch"]
