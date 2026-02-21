FROM mcr.microsoft.com/playwright:v1.58.2-jammy

ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=Etc/UTC

RUN apt-get update && apt-get install -y \
  tzdata \
  xvfb \
  fluxbox \
  tigervnc-standalone-server \
  novnc \
  websockify \
  procps \
  && rm -rf /var/lib/apt/lists/*

ENV DISPLAY=:99
WORKDIR /workspace

# Pre-install playwright globally to avoid runtime delays
RUN npm install -g @playwright/test playwright && \
  npx playwright install --with-deps

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 6080

ENTRYPOINT ["/entrypoint.sh"]
