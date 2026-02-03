# Stage 1: Build the Go binary
FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY services/playwright_scraper/go.mod services/playwright_scraper/go.sum ./
RUN go mod download
COPY services/playwright_scraper/main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o scraper main.go

# Stage 2: Secure packaging stage
FROM alpine:latest AS secure-build

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy packager tools from secure-packager image
COPY --from=stevef1uk/secure-packager:latest /app/packager /usr/local/bin/packager
COPY --from=stevef1uk/secure-packager:latest /app/unpack /usr/local/bin/unpack
COPY --from=stevef1uk/secure-packager:latest /app/issue-token /usr/local/bin/issue-token

RUN chmod +x /usr/local/bin/packager /usr/local/bin/unpack /usr/local/bin/issue-token

# Copy binary from builder
COPY --from=builder /app/scraper /app/scraper

# Copy public keys (build args)
ARG CUSTOMER_PUBLIC_KEY
ARG VENDOR_PUBLIC_KEY
COPY ${CUSTOMER_PUBLIC_KEY} /keys/customer_public.pem
COPY ${VENDOR_PUBLIC_KEY} /keys/vendor_public.pem

# Verify keys exist
RUN test -f /keys/customer_public.pem && test -f /keys/vendor_public.pem

# Package the binary
RUN install -d /payload /out && \
    mv /app/scraper /payload/ && \
    /usr/local/bin/packager \
        -in /payload \
        -out /out \
        -pub /keys/customer_public.pem \
        -vendor-pub /keys/vendor_public.pem \
        -license

# Stage 3: Runtime image with Chromium
FROM debian:bookworm-slim

# Install runtime dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    tzdata \
    bash \
    gosu \
    chromium \
    chromium-driver \
    fonts-liberation \
    libnss3 \
    libxss1 \
    libappindicator3-1 \
    libasound2 \
    libatk-bridge2.0-0 \
    libatk1.0-0 \
    libcups2 \
    libdbus-1-3 \
    libdrm2 \
    libgbm1 \
    libgtk-3-0 \
    libnspr4 \
    libxcomposite1 \
    libxdamage1 \
    libxfixes3 \
    libxrandr2 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy unpack utility from secure-build stage
COPY --from=secure-build /usr/local/bin/unpack /usr/local/bin/unpack

# Copy encrypted package
COPY --from=secure-build /out/encrypted_files.zip ./scraper.enc

# Copy secure entrypoint
COPY services/playwright_scraper/secure_entrypoint.sh /app/entrypoint.sh

# Make scripts executable
RUN chmod +x /app/entrypoint.sh /usr/local/bin/unpack

# Create user for security (but run as root for unpack access)
RUN useradd -m -u 1000 scraper

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD ["/app/entrypoint.sh", "-health-check"] || exit 1

ENTRYPOINT ["/app/entrypoint.sh"]
