# Coherence Monitor Test Suite

## Overview
This document describes the test scripts for the Cross-System Consistency Checking feature.

## Essential Test Scripts (Keep These)

### 1. `test_coherence_monitor.sh`
**Purpose**: Main integration test for coherence monitor
**What it does**:
- Creates test scenarios (conflicting goals, stale goals, activity logs)
- Verifies inconsistencies are detected
- Checks that self-reflection tasks are generated
- Validates curiosity goals are created

**When to use**: Full integration testing, CI/CD

### 2. `check_coherence_status.sh`
**Purpose**: Quick status check of coherence monitor
**What it does**:
- Checks if FSM/coherence monitor is running
- Shows active inconsistencies
- Shows reflection tasks
- Shows coherence goals

**When to use**: Quick health check, troubleshooting

### 3. `check_coherence_feedback_loop.sh`
**Purpose**: Verify the feedback loop (goals → completion → resolution)
**What it does**:
- Checks for goal completion events
- Verifies FSM receives NATS events
- Checks for resolved inconsistencies
- Validates end-to-end resolution flow

**When to use**: Testing the complete resolution cycle

## Diagnostic Scripts (Keep for Troubleshooting)

### 4. `check_coherence_kubernetes_status.sh`
**Purpose**: Comprehensive Kubernetes status check
**What it does**:
- Checks all pods (FSM, Monitor, Redis, Goal Manager)
- Verifies coherence checks are running
- Shows goal conversion status
- Shows resolution events

**When to use**: Deep troubleshooting in Kubernetes environment

### 5. `diagnose_coherence_pipeline.sh`
**Purpose**: Full pipeline diagnostic
**What it does**:
- Checks coherence monitor activity
- Checks Redis data
- Checks Monitor Service processing
- Checks Goal Manager status

**When to use**: When something is broken and you need to find where

## Utility Scripts (Keep for Operations)

### 6. `cleanup_goal_manager_pods.sh`
**Purpose**: Clean up stuck Goal Manager pods
**When to use**: When pods are stuck/terminating

## Scripts to Remove/Archive

These were created during development/debugging and can be removed:

- `diagnose_coherence_monitor.sh` - Superseded by `check_coherence_kubernetes_status.sh`
- `check_coherence_timing.sh` - Functionality in `check_coherence_status.sh`
- `watch_coherence_logs.sh` - Use `kubectl logs -f` directly
- `test_coherence_resolution_feedback.sh` - Superseded by `check_coherence_feedback_loop.sh`
- `check_coherence_resolution_status.sh` - Functionality in `check_coherence_feedback_loop.sh`
- `deep_dive_coherence_goals.sh` - Functionality in `diagnose_coherence_pipeline.sh`
- `diagnose_goal_execution.sh` - Functionality in `diagnose_coherence_pipeline.sh`
- `check_goal_manager_status.sh` - Functionality in `check_coherence_kubernetes_status.sh`
- `check_monitor_sending_goals.sh` - Functionality in `diagnose_coherence_pipeline.sh`
- `check_fsm_coherence_startup.sh` - Functionality in `check_coherence_kubernetes_status.sh`
- `verify_goal_manager_image.sh` - One-time verification, not needed long-term
- `force_local_goal_manager.sh` - Development utility, archive
- `check_coherence_goal_execution.sh` - Functionality in `diagnose_coherence_pipeline.sh`

## Rebuild Scripts (Keep for Development)

- `rebuild_goal_manager.sh` - For rebuilding Goal Manager with changes
- `rebuild_monitor_service.sh` - For rebuilding Monitor Service with changes
- `rebuild_coherence_fix.sh` - For rebuilding all coherence-related components

## Recommended Test Workflow

1. **Quick Check**: `./test/check_coherence_status.sh`
2. **Full Test**: `./test/test_coherence_monitor.sh`
3. **Verify Feedback**: `./test/check_coherence_feedback_loop.sh`
4. **If Issues**: `./test/check_coherence_kubernetes_status.sh` or `./test/diagnose_coherence_pipeline.sh`

## Summary

**Keep (6 scripts)**:
- `test_coherence_monitor.sh` - Main test
- `check_coherence_status.sh` - Quick status
- `check_coherence_feedback_loop.sh` - Feedback verification
- `check_coherence_kubernetes_status.sh` - Comprehensive diagnostic
- `diagnose_coherence_pipeline.sh` - Deep diagnostic
- `cleanup_goal_manager_pods.sh` - Utility

**Archive/Remove (12+ scripts)**:
- All the diagnostic/debugging scripts created during development
- One-time verification scripts
- Scripts with overlapping functionality

