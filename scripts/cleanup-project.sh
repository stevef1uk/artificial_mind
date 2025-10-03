#!/bin/bash

# =============================================================================
# AGI Project Cleanup Script
# =============================================================================
# This script safely reorganizes the project by removing redundant files
# and moving files to their proper locations.
# =============================================================================

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Function to create backup
create_backup() {
    local backup_dir="backup_$(date +%Y%m%d_%H%M%S)"
    print_status "Creating backup in $backup_dir..."
    
    mkdir -p "$backup_dir"
    
    # Backup important files
    cp -r bin/ "$backup_dir/" 2>/dev/null || true
    cp -r config/ "$backup_dir/" 2>/dev/null || true
    cp -r scripts/ "$backup_dir/" 2>/dev/null || true
    cp -r test/ "$backup_dir/" 2>/dev/null || true
    cp *.log "$backup_dir/" 2>/dev/null || true
    
    print_success "Backup created: $backup_dir"
}

# Function to remove log files
remove_log_files() {
    print_status "Removing log files..."
    
    local log_files=(
        "*.log"
        "fsm/*.log"
        "principles/*.log"
        "hdn/*.log"
        "monitor/*.log"
    )
    
    for pattern in "${log_files[@]}"; do
        find . -name "$pattern" -type f -delete 2>/dev/null || true
    done
    
    print_success "Log files removed"
}

# Function to remove duplicate binaries
remove_duplicate_binaries() {
    print_status "Removing duplicate binaries..."
    
    local duplicates=(
        "nats-consumer"
        "nats-producer" 
        "nats-roundtrip"
        "server"
        "bin/hdn-server-original"
        "bin/hdn-server-test"
        "bin/hdn-server-working"
    )
    
    for file in "${duplicates[@]}"; do
        if [ -f "$file" ]; then
            rm -f "$file"
            print_status "Removed: $file"
        fi
    done
    
    print_success "Duplicate binaries removed"
}

# Function to remove old scripts
remove_old_scripts() {
    print_status "Removing old/unused scripts..."
    
    local old_scripts=(
        "split_api.py"
        "split_api_v2.py"
        "split_api_proper.py"
        "extract_handlers.py"
        "clean_handlers.py"
        "run_wiki_summarizer.sh"
        "setup-fsm-only.sh"
    )
    
    for script in "${old_scripts[@]}"; do
        if [ -f "$script" ]; then
            rm -f "$script"
            print_status "Removed: $script"
        fi
    done
    
    print_success "Old scripts removed"
}

# Function to remove temporary files
remove_temp_files() {
    print_status "Removing temporary files..."
    
    # Remove tmp directory
    if [ -d "tmp" ]; then
        rm -rf tmp/
        print_status "Removed tmp/ directory"
    fi
    
    # Remove other temp files
    local temp_files=(
        "report.pdf"
        "*.tmp"
        "*.temp"
    )
    
    for pattern in "${temp_files[@]}"; do
        find . -name "$pattern" -type f -delete 2>/dev/null || true
    done
    
    print_success "Temporary files removed"
}

# Function to reorganize files
reorganize_files() {
    print_status "Reorganizing files..."
    
    # Create directories if they don't exist
    mkdir -p scripts/ test/ config/
    
    # Move scripts to scripts/ directory
    local scripts_to_move=(
        "build-and-push-images.sh"
        "check-docker-images.sh"
        "create_secrets.sh"
        "start_servers.sh"
        "stop_servers.sh"
    )
    
    for script in "${scripts_to_move[@]}"; do
        if [ -f "$script" ]; then
            mv "$script" scripts/
            print_status "Moved: $script -> scripts/"
        fi
    done
    
    # Move test files to test/ directory
    local test_files=(
        "test_*.sh"
        "test_*.go"
        "test_workflow.json"
        "test-news-ingestor.yaml"
        "test-permissions.yaml"
    )
    
    for pattern in "${test_files[@]}"; do
        find . -maxdepth 1 -name "$pattern" -type f -exec mv {} test/ \; 2>/dev/null || true
    done
    
    # Move test_scripts contents to test/
    if [ -d "test_scripts" ]; then
        mv test_scripts/* test/ 2>/dev/null || true
        rmdir test_scripts/ 2>/dev/null || true
        print_status "Moved test_scripts/ contents to test/"
    fi
    
    # Move config files to config/ directory
    local config_files=(
        "config.json"
        "server.yaml"
        "domain.json"
    )
    
    for file in "${config_files[@]}"; do
        if [ -f "$file" ]; then
            mv "$file" config/
            print_status "Moved: $file -> config/"
        fi
    done
    
    print_success "Files reorganized"
}

# Function to update .gitignore
update_gitignore() {
    print_status "Updating .gitignore..."
    
    if [ ! -f ".gitignore" ]; then
        touch .gitignore
    fi
    
    # Add common patterns to .gitignore
    cat >> .gitignore << 'EOF'

# Log files
*.log
**/*.log

# Temporary files
tmp/
*.tmp
*.temp

# Backup directories
backup_*/

# IDE files
.vscode/
.idea/
*.swp
*.swo

# OS files
.DS_Store
Thumbs.db
EOF
    
    print_success ".gitignore updated"
}

# Function to verify cleanup
verify_cleanup() {
    print_status "Verifying cleanup..."
    
    # Check for remaining log files
    local remaining_logs=$(find . -name "*.log" -type f | wc -l)
    if [ "$remaining_logs" -gt 0 ]; then
        print_warning "Found $remaining_logs remaining log files"
    else
        print_success "No log files remaining"
    fi
    
    # Check for duplicate binaries
    local duplicates=(
        "nats-consumer"
        "nats-producer"
        "nats-roundtrip"
        "server"
    )
    
    local found_duplicates=0
    for file in "${duplicates[@]}"; do
        if [ -f "$file" ]; then
            print_warning "Found duplicate binary: $file"
            found_duplicates=1
        fi
    done
    
    if [ "$found_duplicates" -eq 0 ]; then
        print_success "No duplicate binaries found"
    fi
    
    # Check directory structure
    if [ -d "scripts" ] && [ -d "test" ] && [ -d "config" ]; then
        print_success "Directory structure looks good"
    else
        print_error "Directory structure issues detected"
    fi
}

# Function to show summary
show_summary() {
    echo ""
    echo "🧹 Cleanup Summary"
    echo "=================="
    echo ""
    
    # Count files in root
    local root_files=$(find . -maxdepth 1 -type f | wc -l)
    echo "📁 Files in root directory: $root_files"
    
    # Count files in organized directories
    local scripts_count=$(find scripts/ -type f 2>/dev/null | wc -l)
    local test_count=$(find test/ -type f 2>/dev/null | wc -l)
    local config_count=$(find config/ -type f 2>/dev/null | wc -l)
    
    echo "📁 Files in scripts/: $scripts_count"
    echo "📁 Files in test/: $test_count"
    echo "📁 Files in config/: $config_count"
    
    echo ""
    echo "✅ Cleanup completed successfully!"
    echo ""
    echo "Next steps:"
    echo "1. Review the changes: git status"
    echo "2. Test the project: make test"
    echo "3. Update documentation if needed"
    echo "4. Commit the changes: git add . && git commit -m 'Clean up project structure'"
}

# Main function
main() {
    echo "🧹 AGI Project Cleanup"
    echo "======================"
    echo ""
    
    # Check if we're in the right directory
    if [ ! -f "README.md" ] || [ ! -f "go.mod" ]; then
        print_error "Please run this script from the AGI project root directory"
        exit 1
    fi
    
    # Ask for confirmation
    read -p "This will reorganize the project structure. Continue? (y/N): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        print_status "Cleanup cancelled"
        exit 0
    fi
    
    # Create backup
    create_backup
    
    # Perform cleanup
    remove_log_files
    remove_duplicate_binaries
    remove_old_scripts
    remove_temp_files
    reorganize_files
    update_gitignore
    
    # Verify cleanup
    verify_cleanup
    
    # Show summary
    show_summary
}

# Help function
show_help() {
    echo "AGI Project Cleanup Script"
    echo ""
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  --help     Show this help message"
    echo "  --dry-run  Show what would be done without making changes"
    echo ""
    echo "This script will:"
    echo "  - Remove log files and temporary files"
    echo "  - Remove duplicate binaries"
    echo "  - Remove old/unused scripts"
    echo "  - Reorganize files into proper directories"
    echo "  - Update .gitignore"
    echo ""
}

# Parse command line arguments
case "${1:-}" in
    --help|-h)
        show_help
        exit 0
        ;;
    --dry-run)
        print_status "Dry run mode - would perform the following actions:"
        echo "  - Remove log files (*.log)"
        echo "  - Remove duplicate binaries (nats-*, server)"
        echo "  - Remove old scripts (split_api*.py, etc.)"
        echo "  - Remove temporary files (tmp/, *.tmp)"
        echo "  - Move scripts to scripts/ directory"
        echo "  - Move test files to test/ directory"
        echo "  - Move config files to config/ directory"
        echo "  - Update .gitignore"
        exit 0
        ;;
    "")
        main
        ;;
    *)
        print_error "Unknown option: $1"
        show_help
        exit 1
        ;;
esac
