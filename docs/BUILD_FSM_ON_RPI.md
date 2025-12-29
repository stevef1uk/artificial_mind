# Building FSM Docker Container on Raspberry Pi

## Step 1: Pull Latest Changes from Git

On your Raspberry Pi, run these commands:

```bash
# Navigate to your project directory
cd /path/to/artificial_mind

# Fetch all branches from remote
git fetch origin

# Check available branches (you should see the new feature branch)
git branch -a | grep cross-system

# Switch to the new feature branch
git checkout feature/cross-system-consistency-checking

# If the branch doesn't exist locally yet, create it tracking the remote:
git checkout -b feature/cross-system-consistency-checking origin/feature/cross-system-consistency-checking

# Pull the latest changes (if already on the branch)
git pull origin feature/cross-system-consistency-checking
```

## Quick One-Liner (if branch exists on remote)

```bash
git fetch origin && git checkout feature/cross-system-consistency-checking
```

## Step 2: Verify Changes

```bash
# Check that the new files are present
ls -la fsm/coherence_monitor.go
ls -la docs/CROSS_SYSTEM_CONSISTENCY_CHECKING.md
ls -la test/test_coherence_monitor.sh

# Verify the changes are there
git log --oneline -5
git show --stat HEAD
```

## Step 3: Build Docker Container

### Option A: Using the Build Script

```bash
# Make script executable
chmod +x scripts/build-fsm-rpi.sh

# Run the build
./scripts/build-fsm-rpi.sh
```

### Option B: Direct Docker Build

```bash
# Build the FSM container
docker build -f Dockerfile.fsm.secure \
  --build-arg CUSTOMER_PUBLIC_KEY=secure/customer_public.pem \
  --build-arg VENDOR_PUBLIC_KEY=secure/vendor_public.pem \
  -t stevef1uk/fsm-server:secure \
  -t stevef1uk/fsm-server:latest \
  .
```

### Option C: Using the Existing Build Script

```bash
# Build just the FSM server (modify script to build only FSM if needed)
./scripts/build-and-push-images.sh
```

## Step 4: Verify Build

```bash
# Check the image was created
docker images | grep fsm-server

# Test the image (optional)
docker run --rm stevef1uk/fsm-server:secure --help
```

## Troubleshooting

### If you get "branch not found" errors:

```bash
# Fetch all branches
git fetch origin

# List all branches (including remote)
git branch -a

# Create local tracking branch
git checkout -b feature/cross-system-consistency-checking origin/feature/cross-system-consistency-checking
```

### If the branch doesn't exist on remote yet:

Make sure you've pushed the branch from your local machine:

```bash
# On your local machine (Mac):
git push -u origin feature/cross-system-consistency-checking
```

### If you need to merge with master:

```bash
# Switch to master
git checkout master
git pull origin master

# Merge the feature branch
git merge feature/cross-system-consistency-checking

# Resolve any conflicts if needed
# Then build
```

### If secure keys are missing:

The secure Dockerfile requires:
- `secure/customer_public.pem`
- `secure/vendor_public.pem`

If these are missing, you may need to:
1. Copy them from your local machine
2. Or use a non-secure Dockerfile (if one exists)
3. Or create placeholder keys for testing

## Complete Command Sequence

Here's the complete sequence to run on your RPI:

```bash
# 1. Navigate to project
cd ~/artificial_mind  # or wherever your project is

# 2. Fetch and checkout the branch
git fetch origin
git checkout feature/cross-system-consistency-checking

# 3. Verify files
ls fsm/coherence_monitor.go

# 4. Build
docker build -f Dockerfile.fsm.secure \
  --build-arg CUSTOMER_PUBLIC_KEY=secure/customer_public.pem \
  --build-arg VENDOR_PUBLIC_KEY=secure/vendor_public.pem \
  -t stevef1uk/fsm-server:secure .

# 5. Verify
docker images | grep fsm-server
```
