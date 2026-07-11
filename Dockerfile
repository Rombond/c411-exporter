# C411 Exporter - Docker image
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o c411_exporter .

# Final image
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata && \
    mkdir -p /app && \
    chmod 777 /app

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/c411_exporter .

# Create output directory
RUN mkdir -p c411_exports && chmod 777 c411_exports

# Expose port
EXPOSE 8080

# Run the application
CMD ["./c411_exporter", "-config=config.json"]
