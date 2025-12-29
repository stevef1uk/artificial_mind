#!/bin/bash

# Archive/remove redundant coherence monitor test scripts

echo "🧹 Cleaning Up Coherence Monitor Test Scripts"
echo "=============================================="
echo ""

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ARCHIVE_DIR="$SCRIPT_DIR/archive"

# Create archive directory
mkdir -p "$ARCHIVE_DIR"

# Scripts to archive (redundant/debugging scripts)
SCRIPTS_TO_ARCHIVE=(
    "diagnose_coherence_monitor.sh"
    "check_coherence_timing.sh"
    "watch_coherence_logs.sh"
    "test_coherence_resolution_feedback.sh"
    "check_coherence_resolution_status.sh"
    "deep_dive_coherence_goals.sh"
    "diagnose_goal_execution.sh"
    "check_goal_manager_status.sh"
    "check_monitor_sending_goals.sh"
    "check_fsm_coherence_startup.sh"
    "verify_goal_manager_image.sh"
    "force_local_goal_manager.sh"
    "check_coherence_goal_execution.sh"
)

# Scripts to keep
SCRIPTS_TO_KEEP=(
    "test_coherence_monitor.sh"
    "check_coherence_status.sh"
    "check_coherence_feedback_loop.sh"
    "check_coherence_kubernetes_status.sh"
    "diagnose_coherence_pipeline.sh"
    "cleanup_goal_manager_pods.sh"
    "rebuild_goal_manager.sh"
    "rebuild_monitor_service.sh"
    "rebuild_coherence_fix.sh"
)

echo "📦 Scripts to Archive:"
echo "---------------------"
ARCHIVED=0
for script in "${SCRIPTS_TO_ARCHIVE[@]}"; do
    if [ -f "$SCRIPT_DIR/$script" ]; then
        echo "   📦 $script"
        mv "$SCRIPT_DIR/$script" "$ARCHIVE_DIR/" 2>/dev/null && ARCHIVED=$((ARCHIVED + 1))
    fi
done

if [ $ARCHIVED -eq 0 ]; then
    echo "   ℹ️  No scripts to archive (already cleaned up?)"
else
    echo ""
    echo "   ✅ Archived $ARCHIVED script(s) to $ARCHIVE_DIR"
fi

echo ""
echo "✅ Scripts to Keep:"
echo "------------------"
for script in "${SCRIPTS_TO_KEEP[@]}"; do
    if [ -f "$SCRIPT_DIR/$script" ]; then
        echo "   ✅ $script"
    else
        echo "   ⚠️  $script (not found)"
    fi
done

echo ""
echo "📋 Summary:"
echo "----------"
echo "   Archived: $ARCHIVED script(s)"
echo "   Kept: ${#SCRIPTS_TO_KEEP[@]} script(s)"
echo ""
echo "   Archive location: $ARCHIVE_DIR"
echo "   (Scripts can be restored from archive if needed)"
echo ""

