# 🧹 Project Cleanup Plan

This document outlines the cleanup and reorganization plan for the AGI project to remove redundant files and improve maintainability.

## 📋 Identified Redundant Files

### 🗑️ Files to Remove

#### Log Files (Generated)
- `hdn-server.log`
- `monitor-ui.log` 
- `server.log`
- `fsm/fsm-server.log`
- `principles/principles-server.log`

#### Duplicate/Test Binaries
- `bin/hdn-server-original`
- `bin/hdn-server-test`
- `bin/hdn-server-working`
- `nats-consumer` (duplicate of bin/nats-consumer)
- `nats-producer` (duplicate of bin/nats-producer)
- `nats-roundtrip` (duplicate of bin/nats-roundtrip)
- `server` (duplicate binary)

#### Old/Unused Scripts
- `split_api.py`
- `split_api_v2.py`
- `split_api_proper.py`
- `extract_handlers.py`
- `clean_handlers.py`
- `run_wiki_summarizer.sh`
- `setup-fsm-only.sh`

#### Test Files (Move to proper location)
- `test_*.sh` files in root
- `test_*.go` files in root
- `test_workflow.json`
- `test-news-ingestor.yaml`
- `test-permissions.yaml`

#### Old Configuration Files
- `server.yaml.no`
- `debug-news-job.yaml`
- `domain.json` (if replaced by config/domain.json)

#### Temporary Files
- `tmp/` directory contents
- `report.pdf` (if temporary)

### 📁 Files to Reorganize

#### Move to `scripts/` directory
- `build-and-push-images.sh`
- `check-docker-images.sh`
- `create_secrets.sh`
- `start_servers.sh`
- `stop_servers.sh`

#### Move to `test/` directory
- All `test_*.sh` files
- All `test_*.go` files in root
- `test_scripts/` directory contents
- `test_workflow.json`
- `test-news-ingestor.yaml`
- `test-permissions.yaml`

#### Move to `config/` directory
- `config.json`
- `server.yaml`
- `domain.json`

#### Move to `docs/` directory
- `report.pdf` (if documentation)

## 🎯 Cleanup Actions

### Phase 1: Remove Generated Files
```bash
# Remove log files
rm -f *.log
rm -f fsm/*.log
rm -f principles/*.log
rm -f hdn/*.log

# Remove temporary files
rm -rf tmp/
```

### Phase 2: Remove Duplicate Binaries
```bash
# Remove duplicate binaries
rm -f nats-consumer nats-producer nats-roundtrip server
rm -f bin/hdn-server-original bin/hdn-server-test bin/hdn-server-working
```

### Phase 3: Remove Old Scripts
```bash
# Remove old/unused scripts
rm -f split_api*.py extract_handlers.py clean_handlers.py
rm -f run_wiki_summarizer.sh setup-fsm-only.sh
```

### Phase 4: Reorganize Files
```bash
# Move scripts to scripts/ directory
mv build-and-push-images.sh scripts/
mv check-docker-images.sh scripts/
mv create_secrets.sh scripts/
mv start_servers.sh scripts/
mv stop_servers.sh scripts/

# Move test files to test/ directory
mv test_*.sh test/
mv test_*.go test/
mv test_workflow.json test/
mv test-news-ingestor.yaml test/
mv test-permissions.yaml test/
mv test_scripts/* test/
rmdir test_scripts/

# Move config files to config/ directory
mv config.json config/
mv server.yaml config/
mv domain.json config/
```

### Phase 5: Update References
- Update any hardcoded paths in scripts
- Update documentation to reflect new structure
- Update Makefile if needed

## 📊 Expected Results

### Before Cleanup
- **Total files in root**: ~50+ files
- **Redundant binaries**: 4+ duplicates
- **Log files**: 5+ generated files
- **Scattered test files**: 20+ files in various locations

### After Cleanup
- **Total files in root**: ~15 core files
- **Organized structure**: Clear separation of concerns
- **No duplicate files**: Single source of truth
- **Clean git status**: Only source files tracked

## 🔍 Verification Steps

1. **Check git status** - Ensure only source files are tracked
2. **Run tests** - Verify all functionality still works
3. **Check documentation** - Update any broken references
4. **Verify builds** - Ensure all components still build correctly

## ⚠️ Safety Measures

- **Backup first**: Create a backup before cleanup
- **Test after each phase**: Verify functionality at each step
- **Keep git history**: Use git to track changes
- **Document changes**: Update README and docs as needed
