# Build stage
FROM golang:1.22-alpine AS builder

# Install git for any potential dependencies
RUN apk add --no-cache git build-base

# Set the working directory inside the container
WORKDIR /app

# Copy only the go.mod file first to leverage Docker cache
COPY go.mod ./
# If you have a go.sum file, copy it too
# COPY go.sum ./

# Download dependencies (will be cached if go.mod doesn't change)
RUN go mod download

# Copy source code
COPY src/ ./src/

# Build the application with optimizations
# -ldflags="-s -w" strips debug information to reduce binary size
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o csvapi ./src/main.go

# Final stage - using scratch (minimal) image for production
FROM alpine:latest

# Install CA certificates for any potential HTTPS connections
RUN apk --no-cache add ca-certificates tzdata

# Create a non-root user to run the application
RUN adduser -D -g '' appuser
USER appuser

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/csvapi ./

# Expose the port the application runs on
EXPOSE 8080

# Set environment variable
ENV PORT=8080

# Set health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://localhost:8080/ || exit 1

# Run the application
CMD ["./csvapi"] 