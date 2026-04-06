# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /analytics main.go

# Runtime stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates

WORKDIR /

COPY --from=builder /analytics /analytics

EXPOSE 8002

ENV ANALYTICS_PORT=8002
ENV ANALYTICS_HOST=0.0.0.0

ENTRYPOINT ["/analytics"]
