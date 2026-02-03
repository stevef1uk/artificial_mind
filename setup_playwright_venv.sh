#!/bin/bash
# Setup script for Playwright Python virtual environment
# This creates a clean Python environment and installs Playwright

set -e  # Exit on error

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VENV_DIR="$SCRIPT_DIR/playwright_venv"

echo "========================================"
echo "🎭 Playwright Python Setup"
echo "========================================"

# Check if Python 3 is available
if ! command -v python3 &> /dev/null; then
    echo "❌ Python 3 is not installed. Please install Python 3.8 or higher."
    exit 1
fi

PYTHON_VERSION=$(python3 --version)
echo "✅ Found $PYTHON_VERSION"

# Create virtual environment
if [ -d "$VENV_DIR" ]; then
    echo "⚠️  Virtual environment already exists at $VENV_DIR"
    read -p "Do you want to recreate it? (y/N): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo "🗑️  Removing old virtual environment..."
        rm -rf "$VENV_DIR"
    else
        echo "Using existing virtual environment."
    fi
fi

if [ ! -d "$VENV_DIR" ]; then
    echo "📦 Creating virtual environment..."
    python3 -m venv "$VENV_DIR"
    echo "✅ Virtual environment created"
fi

# Activate virtual environment
echo "🔌 Activating virtual environment..."
source "$VENV_DIR/bin/activate"

# Upgrade pip
echo "⬆️  Upgrading pip..."
pip install --upgrade pip > /dev/null

# Install Playwright
echo "📥 Installing Playwright..."
pip install playwright

# Install Playwright browsers
echo "🌐 Installing Playwright browsers (this may take a few minutes)..."
playwright install chromium

echo ""
echo "========================================"
echo "✅ Setup Complete!"
echo "========================================"
echo ""
echo "To use Playwright:"
echo "  1. Activate the virtual environment:"
echo "     source $VENV_DIR/bin/activate"
echo ""
echo "  2. Run the test script:"
echo "     python test_playwright_standalone.py"
echo ""
echo "  3. Or run specific tests:"
echo "     python test_playwright_standalone.py basic https://example.com"
echo "     python test_playwright_standalone.py interactive"
echo "     python test_playwright_standalone.py selectors"
echo ""
echo "To deactivate the virtual environment:"
echo "  deactivate"
echo ""

