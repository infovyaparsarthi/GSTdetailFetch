# Stage 1: Build the Go application binary
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy module definition and Go source code
COPY go.mod ./
COPY main.go ./

# Compile static Linux executable
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o gst-server main.go

# Stage 2: Minimal runtime container
FROM alpine:latest

# Install CA certificates, tzdata, and curl for health checks
RUN apk add --no-cache ca-certificates tzdata curl

WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/gst-server ./
# Copy HTML templates
COPY templates ./templates

# Expose application port
EXPOSE 4192

# Health check configuration for Cloudflare / Coolify
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:4192/health || exit 1

# Start the Go web server
CMD ["./gst-server"]

