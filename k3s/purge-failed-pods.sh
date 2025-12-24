#!/bin/bash

# Script to purge failed pods from Kubernetes cluster
# Usage: ./purge-failed-pods.sh [namespace] [--dry-run] [--all-namespaces]

set -e

NAMESPACE="${1:-agi}"
DRY_RUN=false
ALL_NAMESPACES=false

# Parse arguments
for arg in "$@"; do
    case $arg in
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        --all-namespaces)
            ALL_NAMESPACES=true
            NAMESPACE="--all-namespaces"
            shift
            ;;
        --help|-h)
            echo "Usage: $0 [namespace] [--dry-run] [--all-namespaces]"
            echo ""
            echo "Purge failed pods from Kubernetes cluster"
            echo ""
            echo "Arguments:"
            echo "  namespace          Kubernetes namespace (default: agi)"
            echo "  --dry-run          Show what would be deleted without actually deleting"
            echo "  --all-namespaces    Clean up all namespaces"
            echo "  --help, -h         Show this help message"
            echo ""
            echo "Examples:"
            echo "  $0 agi                    # Purge failed pods in 'agi' namespace"
            echo "  $0 agi --dry-run          # Show what would be deleted"
            echo "  $0 --all-namespaces       # Purge failed pods in all namespaces"
            exit 0
            ;;
    esac
done

echo "🧹 Purging Failed Pods"
echo "======================"
echo "Namespace: $NAMESPACE"
echo "Dry Run: $DRY_RUN"
echo ""

# Get failed pods (including Failed phase and error states)
FAILED_PODS=$(kubectl get pods -n $NAMESPACE -o json 2>/dev/null | \
    jq -r '.items[] | select(.status.phase == "Failed" or 
                              .status.containerStatuses[]?.state.waiting?.reason == "CrashLoopBackOff" or
                              .status.containerStatuses[]?.state.waiting?.reason == "ImagePullBackOff" or
                              .status.containerStatuses[]?.state.waiting?.reason == "ErrImagePull" or
                              .status.containerStatuses[]?.state.waiting?.reason == "CreateContainerError" or
                              .status.containerStatuses[]?.state.waiting?.reason == "InvalidImageName" or
                              .status.containerStatuses[]?.state.terminated?.reason == "Error") | 
                              "\(.metadata.namespace)/\(.metadata.name)"' || echo "")

# Get completed (Succeeded) pods from CronJobs (these accumulate over time)
# We identify them by name containing "cronjob" and phase "Succeeded"
COMPLETED_CRONJOB_PODS=$(kubectl get pods -n $NAMESPACE -o json 2>/dev/null | \
    jq -r '.items[] | select(.status.phase == "Succeeded") | 
           select(.metadata.name | contains("cronjob")) |
           "\(.metadata.namespace)/\(.metadata.name)"' || echo "")

# Get completed (Succeeded) pods from CronJobs (these accumulate over time)
# These are pods with phase "Succeeded" that are owned by Jobs (which are created by CronJobs)
# We identify them by name containing "cronjob" and phase "Succeeded"
COMPLETED_CRONJOB_PODS=$(kubectl get pods $NAMESPACE -o json 2>/dev/null | \
    jq -r '.items[] | select(.status.phase == "Succeeded") | 
           select(.metadata.name | contains("cronjob")) |
           "\(.metadata.namespace)/\(.metadata.name)"')

# Combine failed and completed cronjob pods
ALL_PODS_TO_DELETE=$(echo -e "$FAILED_PODS\n$COMPLETED_CRONJOB_PODS" | grep -v '^$' | sort -u)

# Debug: Check if we found pods
if [ -n "$FAILED_PODS" ] || [ -n "$COMPLETED_CRONJOB_PODS" ]; then
    # We have pods to delete
    HAS_PODS=true
else
    HAS_PODS=false
fi

TOTAL_COUNT=$(echo "$ALL_PODS_TO_DELETE" | grep -v '^$' | wc -l | tr -d ' ' || echo "0")

if [ "$TOTAL_COUNT" = "0" ]; then
    echo "✅ No failed or completed cronjob pods found"
else
    # Count by type (handle empty strings)
    FAILED_COUNT=$(echo "$FAILED_PODS" | grep -v '^$' | wc -l | tr -d ' ' || echo "0")
    COMPLETED_COUNT=$(echo "$COMPLETED_CRONJOB_PODS" | grep -v '^$' | wc -l | tr -d ' ' || echo "0")
    
    echo "Found pods to clean:"
    echo "  - Failed pods: $FAILED_COUNT"
    echo "  - Completed cronjob pods: $COMPLETED_COUNT"
    echo "  - Total: $TOTAL_COUNT"
    echo ""
    
    if [ "$FAILED_COUNT" -gt "0" ]; then
        echo "Failed pods:"
        echo "$FAILED_PODS" | while IFS= read -r pod; do
            if [ -n "$pod" ]; then
                namespace=$(echo "$pod" | cut -d'/' -f1)
                name=$(echo "$pod" | cut -d'/' -f2)
                reason=$(kubectl get pod "$name" -n "$namespace" -o jsonpath='{.status.containerStatuses[0].state.waiting.reason}{.status.containerStatuses[0].state.terminated.reason}{.status.phase}' 2>/dev/null || echo "Unknown")
                echo "  - $pod (Reason: $reason)"
            fi
        done
        echo ""
    fi
    
    if [ "$COMPLETED_COUNT" -gt "0" ]; then
        echo "Completed cronjob pods (old runs):"
        echo "$COMPLETED_CRONJOB_PODS" | head -10 | while IFS= read -r pod; do
            if [ -n "$pod" ]; then
                echo "  - $pod"
            fi
        done
        if [ "$COMPLETED_COUNT" -gt "10" ]; then
            echo "  ... and $((COMPLETED_COUNT - 10)) more"
        fi
        echo ""
    fi
    
    if [ "$DRY_RUN" = true ]; then
        echo "🔍 DRY RUN: Would delete the above pods"
        echo "Run without --dry-run to actually delete them"
    else
        echo "🗑️  Deleting pods..."
        deleted=0
        echo "$ALL_PODS_TO_DELETE" | while IFS= read -r pod; do
            if [ -n "$pod" ]; then
                namespace=$(echo "$pod" | cut -d'/' -f1)
                name=$(echo "$pod" | cut -d'/' -f2)
                kubectl delete pod "$name" -n "$namespace" --grace-period=0 --force 2>/dev/null && deleted=$((deleted+1)) || true
            fi
        done
        echo "✅ Deleted $(echo "$ALL_PODS_TO_DELETE" | grep -c . || echo "0") pod(s)"
    fi
fi

echo ""

# Also clean up completed jobs (optional)
echo "📋 Checking for completed jobs..."
COMPLETED_JOBS=$(kubectl get jobs -n $NAMESPACE -o json 2>/dev/null | \
    jq -r '.items[] | select(.status.succeeded == 1 or .status.failed > 0) | "\(.metadata.namespace)/\(.metadata.name)"')

if [ -z "$COMPLETED_JOBS" ]; then
    echo "✅ No completed jobs to clean up"
else
    echo "Found completed/failed jobs:"
    echo "$COMPLETED_JOBS" | while IFS= read -r job; do
        if [ -n "$job" ]; then
            echo "  - $job"
        fi
    done
    echo ""
    
    if [ "$DRY_RUN" = true ]; then
        echo "🔍 DRY RUN: Would delete the above jobs"
    else
        read -p "Delete completed jobs? (y/N): " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            echo "$COMPLETED_JOBS" | while IFS= read -r job; do
                if [ -n "$job" ]; then
                    namespace=$(echo "$job" | cut -d'/' -f1)
                    name=$(echo "$job" | cut -d'/' -f2)
                    echo "  Deleting $namespace/$name..."
                    kubectl delete job "$name" -n "$namespace" 2>/dev/null || true
                fi
            done
            echo "✅ Jobs deleted"
        fi
    fi
fi

echo ""
echo "📊 Summary:"
if [ "$DRY_RUN" = false ]; then
    echo "  Failed pods remaining: $(kubectl get pods -n $NAMESPACE -o json 2>/dev/null | jq -r '[.items[] | select(.status.phase == "Failed" or .status.containerStatuses[]?.state.waiting?.reason == "CrashLoopBackOff")] | length')"
    echo "  Total pods: $(kubectl get pods -n $NAMESPACE --no-headers 2>/dev/null | wc -l | tr -d ' ')"
fi

