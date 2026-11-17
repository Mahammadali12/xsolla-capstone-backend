# Build the worker, then run it on a minimal image.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/worker ./cmd/worker

FROM alpine:3.22
# ca-certificates: TLS for later broker/email clients. wget: container healthcheck.
RUN apk add --no-cache ca-certificates wget
WORKDIR /app
COPY --from=build /out/worker ./worker
COPY config ./config
EXPOSE 8081
CMD ["./worker"]
