# Adding TELEGRAM_CHAT_ID to k3s Secret

## Option 1: Using kubectl patch (Recommended)

```bash
# Add or update TELEGRAM_CHAT_ID
kubectl patch secret telegram-bot-secret -n agi --type='json' \
  -p='[{"op": "add", "path": "/data/TELEGRAM_CHAT_ID", "value": "'$(echo -n "-1003712575871" | base64)'"}]'
```

If you get an error that the key already exists, use `replace` instead:

```bash
kubectl patch secret telegram-bot-secret -n agi --type='json' \
  -p='[{"op": "replace", "path": "/data/TELEGRAM_CHAT_ID", "value": "'$(echo -n "-1003712575871" | base64)'"}]'
```

## Option 2: Using kubectl create (Simpler)

```bash
# This will merge with existing secret keys
kubectl create secret generic telegram-bot-secret -n agi \
  --from-literal=TELEGRAM_CHAT_ID="-1003712575871" \
  --dry-run=client -o yaml | kubectl apply -f -
```

## Option 3: Manual base64 encoding

```bash
# Encode the channel ID
echo -n "-1003712575871" | base64
# Output: LTMwMDM3MTI1NzU4NzE=

# Then edit the secret directly
kubectl edit secret telegram-bot-secret -n agi
# Add this line under data:
#   TELEGRAM_CHAT_ID: LTMwMDM3MTI1NzU4NzE=
```

## Verify it was added:

```bash
kubectl get secret telegram-bot-secret -n agi -o jsonpath='{.data.TELEGRAM_CHAT_ID}' | base64 -d && echo
```

Should output: `-1003712575871`

## After adding, restart HDN server:

```bash
kubectl rollout restart deployment/hdn-server-rpi58 -n agi
```


