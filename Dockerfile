# Build the API, then run it on a minimal image.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/server ./cmd/server

FROM alpine:3.22
# ca-certificates: TLS to S3 and Google. wget: container healthcheck.
RUN apk add --no-cache ca-certificates wget
WORKDIR /app
COPY --from=build /out/server ./server
# MIGRATIONS_DIR defaults to internal/db/migrations, so keep that path.
COPY --from=build /src/internal/db/migrations ./internal/db/migrations
EXPOSE 8080
CMD ["./server"]
