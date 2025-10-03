#!/bin/bash

# =============================================================================
# Add License Headers to Source Files
# =============================================================================
# This script adds the MIT License with Attribution Requirement header to
# source files in the project.
# =============================================================================

set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

print_status() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

# License header template
LICENSE_HEADER='/*
 * AGI Project - AI Building AI
 * 
 * Copyright (c) 2025 Steven Fisher
 * 
 * This software is licensed under the MIT License with Attribution Requirement.
 * See LICENSE file for complete terms.
 * 
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 * 
 * 1. The above copyright notice and this permission notice shall be included in all
 *    copies or substantial portions of the Software.
 * 
 * 2. Any derivative works, modifications, or distributions of this Software must
 *    include the original copyright notice and this license file in their entirety.
 * 
 * 3. The name "Steven Fisher" must be prominently displayed in any documentation,
 *    about pages, or credits sections of any software that uses or is derived from
 *    this Software.
 * 
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */'

# Function to add license header to a file
add_license_header() {
    local file="$1"
    
    # Check if file already has license header
    if head -n 5 "$file" | grep -q "Copyright (c) 2025 Steven Fisher"; then
        print_warning "License header already exists in $file"
        return 0
    fi
    
    # Create temporary file with license header
    local temp_file=$(mktemp)
    
    # Add license header
    echo "$LICENSE_HEADER" > "$temp_file"
    echo "" >> "$temp_file"
    
    # Add original file content
    cat "$file" >> "$temp_file"
    
    # Replace original file
    mv "$temp_file" "$file"
    
    print_status "Added license header to $file"
}

# Main function
main() {
    print_status "Adding license headers to source files..."
    
    # Find all Go files
    find . -name "*.go" -not -path "./vendor/*" -not -path "./.git/*" | while read -r file; do
        add_license_header "$file"
    done
    
    # Find all JavaScript files
    find . -name "*.js" -not -path "./node_modules/*" -not -path "./.git/*" | while read -r file; do
        add_license_header "$file"
    done
    
    # Find all TypeScript files
    find . -name "*.ts" -not -path "./node_modules/*" -not -path "./.git/*" | while read -r file; do
        add_license_header "$file"
    done
    
    # Find all Python files
    find . -name "*.py" -not -path "./.git/*" | while read -r file; do
        add_license_header "$file"
    done
    
    print_status "License headers added successfully!"
    print_status "Please review the changes before committing."
}

# Help function
show_help() {
    echo "Add License Headers to Source Files"
    echo ""
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  --help     Show this help message"
    echo "  --dry-run  Show what would be done without making changes"
    echo ""
    echo "This script adds the MIT License with Attribution Requirement header"
    echo "to source files in the project."
}

# Parse command line arguments
case "${1:-}" in
    --help|-h)
        show_help
        exit 0
        ;;
    --dry-run)
        print_status "Dry run mode - would add license headers to:"
        find . -name "*.go" -o -name "*.js" -o -name "*.ts" -o -name "*.py" | \
        grep -v -E "(vendor/|node_modules/|\.git/)" | while read -r file; do
            if ! head -n 5 "$file" | grep -q "Copyright (c) 2025 Steven Fisher"; then
                echo "  $file"
            fi
        done
        exit 0
        ;;
    "")
        main
        ;;
    *)
        print_warning "Unknown option: $1"
        show_help
        exit 1
        ;;
esac
