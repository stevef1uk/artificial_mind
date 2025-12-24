#!/bin/bash

# Create secrets for AGI system
# This script creates all the required secrets for the AGI system to run

set -e

NAMESPACE="agi"
SECURE_DIR="/home/pi/dev/artificial_mind/secure"
SSH_DIR="/home/pi/.ssh"

echo "Creating secrets for AGI system in namespace: $NAMESPACE"

# Check if namespace exists
if ! kubectl get namespace $NAMESPACE > /dev/null 2>&1; then
    echo "Creating namespace: $NAMESPACE"
    kubectl create namespace $NAMESPACE
fi

# Create secure-customer-private secret
echo "Creating secure-customer-private secret..."
kubectl create secret generic secure-customer-private -n $NAMESPACE \
    --from-file=customer_private.pem=$SECURE_DIR/customer_private.pem \
    --dry-run=client -o yaml | kubectl apply -f -

# Create secure-vendor secret
echo "Creating secure-vendor secret..."
kubectl create secret generic secure-vendor -n $NAMESPACE \
    --from-file=token=$SECURE_DIR/token.txt \
    --dry-run=client -o yaml | kubectl apply -f -

# Create ssh-keys secret
echo "Creating ssh-keys secret..."
kubectl create secret generic ssh-keys -n $NAMESPACE \
    --from-file=id_rsa=$SSH_DIR/id_rsa
    --from-file=id_rsa.pub=$SSH_DIR/id_rsa.pub \
    --from-file=known_hosts=$SSH_DIR/known_hosts \
    --dry-run=client -o yaml | kubectl apply -f -

echo "All secrets created successfully!"
echo ""
echo "Created secrets:"
kubectl get secrets -n $NAMESPACE
