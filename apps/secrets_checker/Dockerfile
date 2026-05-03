FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod ./
# No go.sum yet, so we'll skip copying it if it doesn't exist
RUN go mod download

COPY . .
# Build the binary for arm64 (RPi) and amd64
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o bin/secrets_checker .

# Use prebuilt secure-packager image
FROM stevef1uk/secure-packager:latest AS packager

FROM alpine:latest AS secure-build
RUN apk add --no-cache ca-certificates tzdata && update-ca-certificates
WORKDIR /app
# Copy packager tools
COPY --from=packager /app/packager /usr/local/bin/packager
COPY --from=packager /app/unpack /usr/local/bin/unpack
COPY --from=packager /app/issue-token /usr/local/bin/issue-token
RUN chmod +x /usr/local/bin/packager /usr/local/bin/unpack /usr/local/bin/issue-token

# Copy built binary from builder
COPY --from=builder /app/bin/secrets_checker /app/secrets_checker

# Expect secure keys to be available in the build context (root)
# We assume the build is run from the root of artificial_mind
ARG CUSTOMER_PUBLIC_KEY=secure/customer_public.pem
ARG VENDOR_PUBLIC_KEY=secure/vendor_public.pem
COPY ${CUSTOMER_PUBLIC_KEY} /keys/customer_public.pem
COPY ${VENDOR_PUBLIC_KEY} /keys/vendor_public.pem

# Produce encrypted payload
RUN install -d /payload /out \
  && mv /app/secrets_checker /payload/ \
  && /usr/local/bin/packager \
  -in /payload \
  -out /out \
  -pub /keys/customer_public.pem \
  -zip=true \
  -license \
  -vendor-pub /keys/vendor_public.pem

# Final runtime image
FROM alpine:latest
RUN apk add --no-cache \
  ca-certificates \
  git \
  bash \
  tzdata && \
  update-ca-certificates
WORKDIR /app

# runtime unpack tool and encrypted payload
COPY --from=secure-build /usr/local/bin/unpack /usr/local/bin/unpack
COPY --from=secure-build /out/encrypted_files.zip ./secrets_checker.enc

# app config
COPY apps/secrets_checker/config.json ./config.json

# entrypoint (from root scripts)
COPY scripts/secure_entrypoint.sh ./entrypoint.sh
RUN chmod +x ./entrypoint.sh /usr/local/bin/unpack

# Set the binary name for the secure entrypoint
ENV SECURE_BINARY_NAME=secrets_checker
ENTRYPOINT ["/app/entrypoint.sh"]
