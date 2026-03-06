# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git make

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build
RUN make build-linux-amd64

# Runtime stage
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 flora-agent && \
    adduser -u 1000 -G flora-agent -s /bin/sh -D flora-agent

# Create directories
RUN mkdir -p /etc/flora-agent /var/lib/flora-agent /data/recordings && \
    chown -R flora-agent:flora-agent /var/lib/flora-agent /data/recordings

# Copy binary
COPY --from=builder /app/bin/flora-agent-linux-amd64 /usr/local/bin/flora-agent

# Copy default config
COPY configs/agent.example.yaml /etc/flora-agent/agent.yaml

# Switch to non-root user
USER flora-agent

# Volumes
VOLUME ["/data/recordings", "/var/lib/flora-agent", "/etc/flora-agent"]

# Health check using the health endpoint
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O /dev/null http://localhost:8080/livez || exit 1

ENTRYPOINT ["flora-agent"]
CMD ["run", "--config", "/etc/flora-agent/agent.yaml"]
