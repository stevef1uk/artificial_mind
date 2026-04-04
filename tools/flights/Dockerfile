# Stage 1: Build the Go binary
FROM golang:1.25.8-bookworm AS builder

# Set the working directory
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

# Stage 2: Runtime image with Chromium and Tesseract
FROM debian:bookworm-slim

# Install runtime dependencies: 
# 1. Tesseract for OCR
# 2. Chromium and its dependencies for Playwright
# 3. CA Certificates for HTTPS requests
RUN apt-get update && apt-get install -y \
    tesseract-ocr \
    tesseract-ocr-eng \
    libtesseract5 \
    ca-certificates \
    curl \
    gnupg \
    procps \
    # Playwright dependencies for Chromium
    libnss3 \
    libnspr4 \
    libatk1.0-0 \
    libatk-bridge2.0-0 \
    libcups2 \
    libdrm2 \
    libxkbcommon0 \
    libxcomposite1 \
    libxdamage1 \
    libxext6 \
    libxfixes3 \
    libxrandr2 \
    libgbm1 \
    libpango-1.0-0 \
    libcairo2 \
    libasound2 \
    chromium \
    && rm -rf /var/lib/apt/lists/*

# Set environment variables for Playwright
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
ENV PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH=/usr/bin/chromium

# Set the working directory
WORKDIR /app

# Copy the built binary from the builder stage
COPY --from=builder /app/flight_mcp /app/flight_mcp

# Expose the port (default 8086)
EXPOSE 8086

# Run the MCP server
ENTRYPOINT ["/app/flight_mcp"]
CMD ["--transport", "sse", "--port", "8086"]
