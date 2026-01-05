#!/bin/bash
# PSync Daemon Integration Test
# This script starts two daemon instances and verifies file synchronization.

set -e

echo "=== PSync Integration Test ==="

# Create temp directories
DIR_A=$(mktemp -d)
DIR_B=$(mktemp -d)
echo "Peer A directory: $DIR_A"
echo "Peer B directory: $DIR_B"

# Cleanup function
cleanup() {
    echo "Cleaning up..."
    kill $PID_A $PID_B 2>/dev/null || true
    rm -rf "$DIR_A" "$DIR_B"
}
trap cleanup EXIT

# Build daemon
echo "Building daemon..."
cd "$(dirname "$0")/.."
go build -o /tmp/psync-daemon ./cmd/daemon

# Start signal server (assumes it's already running or start it here)
# For now, we assume signal server is running on :8080

# Start Peer A
echo "Starting Peer A..."
/tmp/psync-daemon -root "$DIR_A" -id "peer-a" -group "test-group" &
PID_A=$!

# Start Peer B
echo "Starting Peer B..."
/tmp/psync-daemon -root "$DIR_B" -id "peer-b" -group "test-group" &
PID_B=$!

# Wait for daemons to initialize
sleep 2

# Create a file on Peer A
echo "Creating test file on Peer A..."
echo "Hello from peer-a" > "$DIR_A/test-file.txt"

# Wait for sync
echo "Waiting for sync..."
sleep 3

# Verify file exists on Peer B
echo "Verifying sync on Peer B..."
if [ -f "$DIR_B/test-file.txt" ]; then
    CONTENT=$(cat "$DIR_B/test-file.txt")
    if [ "$CONTENT" = "Hello from peer-a" ]; then
        echo "✓ File synced correctly!"
    else
        echo "✗ File content mismatch!"
        echo "Expected: 'Hello from peer-a'"
        echo "Got: '$CONTENT'"
        exit 1
    fi
else
    echo "✗ File not found on Peer B!"
    ls -la "$DIR_B"
    exit 1
fi

# Test bidirectional sync
echo "Creating test file on Peer B..."
echo "Hello from peer-b" > "$DIR_B/response.txt"

sleep 3

if [ -f "$DIR_A/response.txt" ]; then
    echo "✓ Bidirectional sync works!"
else
    echo "✗ Bidirectional sync failed!"
    exit 1
fi

echo ""
echo "=== All integration tests passed! ==="
