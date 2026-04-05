# Stage 1: Build the Go binary
FROM golang:1.25.8-bookworm AS builder

# Set the working directory
ENV TESSDATA_PREFIX=/usr/share/tesseract-ocr/5/
WORKDIR /app

# Enable CGO for gosseract
ENV CGO_ENABLED=1

# Install system dependencies (needed for OCR library headers)
RUN apt-get update && apt-get install -y \
    libtesseract-dev \
    tesseract-ocr \
    && rm -rf /var/lib/apt/lists/*

# Copy go mod and sum files specifically from the flights tool directory
COPY tools/flights/go.mod tools/flights/go.sum ./
RUN go mod download

# Copy the entire flights tool source code
COPY tools/flights/ .

# Build the application
RUN go build -o flight_mcp .

# Stage 2: Slim Runtime image with only Tesseract
FROM debian:bookworm-slim

# Install only the runtime dependencies for OCR (Tesseract)
# All Chromium and Playwright dependencies have been removed
RUN apt-get update && apt-get install -y \
    tesseract-ocr \
    tesseract-ocr-eng \
    libtesseract5 \
    ca-certificates \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Set the working directory
ENV TESSDATA_PREFIX=/usr/share/tesseract-ocr/5/
WORKDIR /app

# Copy the built binary from the builder stage
COPY --from=builder /app/flight_mcp /app/flight_mcp

# Expose the port (default 8086)
EXPOSE 8086

# Run the MCP server
ENTRYPOINT ["/app/flight_mcp"]
CMD ["--transport", "http", "--port", "8086"]
