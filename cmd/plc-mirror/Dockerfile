FROM golang:1.22.3 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN go build -trimpath ./cmd/plc-mirror

FROM alpine:latest AS certs
RUN apk --update add ca-certificates

FROM debian:stable-slim
RUN apt update && apt install -y curl
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /app/plc-mirror .
ENTRYPOINT ["./plc-mirror"]
