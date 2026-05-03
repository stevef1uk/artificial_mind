# Secrets Checker 🛡️

An autonomous, secure, and containerized Git repository secret-scanning system with Telegram alerting.

## Features
- **Differential Scanning**: Only scans new commits since the last run.
- **Internal Engine**: High-performance regex-based scanning (no network dependencies).
- **Telegram Alerting**: Real-time summary alerts for found secrets.
- **Secure Image Protection**: Encrypted binary at rest, decrypted at runtime via `secure-packager`.
- **Persistent State**: Maintains scan state on a Kubernetes PVC.

## 🛠️ Build Instructions

The build **must** be executed from the root of the `artificial_mind` repository to correctly include the secure keys and entrypoint scripts.

```bash
cd ~/dev/artificial_mind
git pull
docker build -t stevef1uk/secrets-checker:latest -f apps/secrets_checker/Dockerfile .
docker push stevef1uk/secrets-checker:latest
```

## 🚀 Deployment (k3s)

### 1. Prerequisite Secrets
Ensure the following secrets exist in the `agi` namespace:
- `secure-customer-private`: Your private key for decryption.
- `secure-vendor`: Your license token for the unpacker.
- `telegram-bot-secret`: Bot token and chat ID.
- `github-token` (Optional): To avoid API rate limits.

### 2. Apply Manifests
Apply the CronJob and Persistent Volume Claim:
```bash
kubectl apply -f apps/secrets_checker/k3s/cronjob.yaml
```

### 3. Manual Test Run
To trigger a scan immediately:
```bash
kubectl delete job secrets-checker-manual-test -n agi || true
kubectl create job --from=cronjob/secrets-checker secrets-checker-manual-test -n agi
```

## 📊 Monitoring
- **Logs**: `kubectl logs -n agi -l job-name=secrets-checker-manual-test`
- **PVC Content**: State and clones are stored on the `secrets-checker-pvc`.

## ⚙️ Configuration
Modify `config.json` to change:
- `github_users`: List of users/orgs to monitor.
- `concurrency`: Number of simultaneous repository scans.
- `stale_threshold_days`: Ignore repos not pushed to in X days.
