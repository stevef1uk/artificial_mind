# Manual Secret Recovery
This document contains the commands required to restore sensitive secrets that are not stored in the git repository.

## 1. Secure Vendor Token (Authentication)
This secret is used as the `X-Webhook-Secret` for n8n authentication.

```bash
kubectl create secret generic secure-vendor -n agi \
  --from-literal=token="2026-12-31:SJFisher:stevef@gmail.com:NOFERNET:Kxi-gy6GfvLshuPJgIhSVdIznmtVRGPKpVLJCuHXVDrLi7haH3vnCrnuv5ysf9uFit_s7uDOCs8QlJcg_C3SUjYr65_yDIYG75gqW22v7XLz1O6o8YUbgjQRbmRvkPinZjHTmBgyqb6tKd6hKqINMMYNKYiGOOUlcJqwlng96f79mXMla9LdY26mq7VQUQsQyk151UXcf3BIvbA4OpyqkqajsdvlDITlZFdbsI5AfX2pO9LqvrY9CYYRSePTxWx0sRWZROSvql30fBc1N-iLJwsYEmhWGeXrCUmh9gUoYKwFu1CtFjcpFKnCHrIEPjhT4SNMrxPcCPme7HpY4HIdqw==" \
  --dry-run=client -o yaml | kubectl apply -f -
```

## 2. N8N Webhook URL
This URL is sensitive and is injected into the `llm-config` secret manually.

**Production URL:** `https://k3s.sjfisher.com/webhook/6f632b61-6b01-4910-991d-3a378b1e653a`

```bash
# First apply the base config from the repo
kubectl apply -f k3s/llm-config-secret.yaml

# Then patch it with the production URL
kubectl patch secret llm-config -n agi --type='json' -p="[{\"op\": \"add\", \"path\": \"/data/N8N_WEBHOOK_URL\", \"value\": \"$(echo -n 'https://k3s.sjfisher.com/webhook/6f632b61-6b01-4910-991d-3a378b1e653a' | base64 | tr -d '\n')\"}]"
```

## 3. Restart Services
After applying secrets, restart the HDN server to pick up the changes:

```bash
kubectl rollout restart deployment hdn-server-rpi58 -n agi
```
