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

# Install CA certificates for secure HTTPS outgoing requests
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/gst-server ./
# Copy HTML templates
COPY templates ./templates

# Expose application port
EXPOSE 4192

# Start the Go web server
CMD ["./gst-server"]
