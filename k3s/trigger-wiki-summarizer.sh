#!/bin/bash

# Manually trigger a wiki-summarizer job

set -e

NAMESPACE="agi"
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_status() {
    echo -e "${BLUE}[TRIGGER]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

echo "=========================================="
echo "Manual Wiki-Summarizer Trigger"
echo "=========================================="
echo ""

# Create a manual job from the cronjob
JOB_NAME="wiki-summarizer-manual-$(date +%s)"
print_status "Creating manual job: $JOB_NAME"

if kubectl create job "$JOB_NAME" --from=cronjob/wiki-summarizer-cronjob -n $NAMESPACE 2>&1; then
    print_success "Job created: $JOB_NAME"
    
    # Wait for pod to start
    print_status "Waiting for pod to start..."
    sleep 5
    
    POD_NAME=$(kubectl get pods -n $NAMESPACE -l job-name="$JOB_NAME" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
    if [ -z "$POD_NAME" ]; then
        print_error "Pod not found for job $JOB_NAME"
        exit 1
    fi
    
    print_success "Pod running: $POD_NAME"
    echo ""
    
    # Show logs
    print_status "Following logs (Ctrl+C to stop following, job will continue)..."
    echo ""
    kubectl logs -n $NAMESPACE "$POD_NAME" -f
    
else
    print_error "Failed to create job"
    exit 1
fi

echo ""
echo "=========================================="
echo "Job Complete"
echo "=========================================="
echo ""
echo "To check job status:"
echo "  kubectl get job $JOB_NAME -n $NAMESPACE"
echo ""
echo "To view logs:"
echo "  kubectl logs -n $NAMESPACE -l job-name=$JOB_NAME"
echo ""
echo "To delete the job:"
echo "  kubectl delete job $JOB_NAME -n $NAMESPACE"
echo ""

