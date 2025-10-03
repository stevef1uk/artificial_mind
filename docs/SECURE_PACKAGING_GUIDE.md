# 🔐 Secure Packaging Guide

Create the required security files for the AGI project.

## Required Artifacts

Create these files in the `secure/` directory:
- `customer_private.pem`
- `customer_public.pem`
- `vendor_private.pem`
- `vendor_public.pem`
- `token.txt`

## Commands

```bash
# Create secure directory
mkdir -p secure/

# Customer keypair
openssl genrsa -out secure/customer_private.pem 2048
openssl rsa -in secure/customer_private.pem -pubout -out secure/customer_public.pem

# Vendor keypair (only needed for licensing mode; signs license tokens)
openssl genrsa -out secure/vendor_private.pem 2048
openssl rsa -in secure/vendor_private.pem -pubout -out secure/vendor_public.pem

# Create token.txt (add your token content)
echo "your-token-content-here" > secure/token.txt
```

That's it! The `secure/` directory is excluded from git.