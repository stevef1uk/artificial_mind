FROM golang:1.23-alpine AS builder
WORKDIR /app

# Copy directories explicitly to bypass any context issues
COPY apps/ /app/apps/
COPY scripts/ /app/scripts/
COPY go.mod ./
COPY . .

# Build the binary
RUN cd apps/secrets_checker && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/secrets_checker .

# Verify and move assets to root
RUN ls -l /app/apps/secrets_checker/config.json && \
    cp /app/apps/secrets_checker/config.json /app/config.json && \
    cp /app/scripts/secure_entrypoint.sh /app/entrypoint.sh

# Use prebuilt secure-packager image
FROM stevef1uk/secure-packager:latest AS packager

FROM alpine:latest AS secure-build
RUN apk add --no-cache ca-certificates tzdata bash && update-ca-certificates
WORKDIR /app
COPY --from=packager /app/packager /usr/local/bin/packager
COPY --from=packager /app/unpack /usr/local/bin/unpack
COPY --from=packager /app/issue-token /usr/local/bin/issue-token
RUN chmod +x /usr/local/bin/packager /usr/local/bin/unpack /usr/local/bin/issue-token

# Copy built binary from builder
COPY --from=builder /app/secrets_checker /app/secrets_checker

# Copy secure keys
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

# app config (copying from flat location in builder)
COPY --from=builder /app/config.json ./config.json

# entrypoint (copying from flat location in builder)
COPY --from=builder /app/entrypoint.sh ./entrypoint.sh
RUN chmod +x ./entrypoint.sh /usr/local/bin/unpack

# Set the binary name for the secure entrypoint
ENV SECURE_BINARY_NAME=secrets_checker
ENTRYPOINT ["/app/entrypoint.sh"]
