#!/bin/bash
# Butler CLI Setup Script
# Run this from the butler-cli directory

set -e

echo "=== Butler CLI Setup ==="

# Check we're in the right directory
if [ ! -f "go.mod" ]; then
    echo "Error: Run this from the butler-cli directory"
    exit 1
fi

# Check butler-api exists
if [ ! -d "../butler-api" ]; then
    echo "Error: butler-api directory not found at ../butler-api"
    echo "Expected structure:"
    echo "  butlerdotdev/"
    echo "    ├── butler-api/"
    echo "    ├── butler-cli/  (you are here)"
    echo "    └── ..."
    exit 1
fi

echo "✓ Directory structure looks good"

# Initialize module if needed
echo ""
echo "=== Running go mod tidy ==="
go mod tidy

echo ""
echo "=== Building CLIs ==="
make build

echo ""
echo "=== Testing CLIs ==="
./bin/butleradm version
./bin/butlerctl version

echo ""
echo "=== Setup Complete! ==="
echo ""
echo "Next steps:"
echo "  1. Run: ./bin/butleradm bootstrap harvester --help"
echo "  2. Create your bootstrap.yaml (see configs/examples/)"
echo "  3. Run: ./bin/butleradm bootstrap harvester --config bootstrap.yaml --dry-run"
echo ""
echo "Install to PATH:"
echo "  make install-local   # installs to ~/bin"
echo "  sudo make install    # installs to /usr/local/bin"
