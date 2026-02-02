# Quick Fix for Token Signature Error

## The Problem
The error `token signature invalid: crypto/rsa: verification error` means:
- The Docker image was built with a specific `vendor_public.pem` key
- The token in Kubernetes was signed with a different `vendor_private.pem` key
- They don't match, so verification fails

## Solution

### Option 1: Regenerate Token with Correct Key (Recommended)

1. **Make sure you have the correct vendor_private.pem** that matches the vendor_public.pem used to build the image:
   ```bash
   cd ~/dev/artificial_mind/k3s
   ./generate-vendor-token.sh /home/stevef/dev/agi/secure
   ```

2. **Update Kubernetes secrets:**
   ```bash
   ./update-secrets.sh /home/stevef/dev/agi/secure
   ```

3. **Verify the token was updated:**
   ```bash
   kubectl get secret secure-vendor -n agi -o jsonpath='{.data.token}' | base64 -d | head -c 50
   ```

4. **Wait for next cronjob run or manually trigger:**
   ```bash
   # Delete existing failed jobs
   kubectl delete job -n agi -l job-name=wiki-bootstrapper-cronjob
   
   # Or wait for next scheduled run (every 10 minutes)
   ```

### Option 2: Use the Fix Script

Run the comprehensive fix script:
```bash
cd ~/dev/artificial_mind/k3s
./fix-token-signature.sh /home/stevef/dev/agi/secure
```

### Option 3: If Keys Don't Match - Rebuild Image

If the vendor_private.pem you have doesn't match the vendor_public.pem used to build the image:

1. **Find out which vendor_public.pem was used to build the image** (check build logs or Dockerfile)
2. **Either:**
   - Rebuild the image with your current vendor_public.pem, OR
   - Use the vendor_private.pem that matches the one used in the image

## Verification

After updating, check the pod logs:
```bash
# Watch for new cronjob runs
kubectl get pods -n agi -l app=wiki-bootstrapper --watch

# Check logs of the latest pod
kubectl logs -n agi -l app=wiki-bootstrapper --tail=50
```

The error should be gone if the token matches the image's embedded vendor_public.pem.

## Common Issues

1. **Token has whitespace**: The token should be a single line with no newlines
2. **Wrong key pair**: Make sure vendor_private.pem and vendor_public.pem are a matching pair
3. **Image rebuilt with different key**: If you rebuilt the image, you need a new token signed with the new key







