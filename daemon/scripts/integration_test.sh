bin/bash
# PSync Daemon Integration Test
# This script starts two daemon instances and verifies file synchronization.

set -e

echo "=== PSync Integration Test ==="

# Get the script's directory
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DAEMON_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Create temp directories under scripts/tmp (safer than system temp)
TMP_DIR="$SCRIPT_DIR/tmp"
mkdir -p "$TMP_DIR"
DIR_A="$TMP_DIR/peer-a"
DIR_B="$TMP_DIR/peer-b"
mkdir -p "$DIR_A" "$DIR_B"

echo "Peer A directory: $DIR_A"
echo "Peer B directory: $DIR_B"

# Cleanup function - only removes within our controlled tmp directory
cleanup() {
    echo "Cleaning up..."
    kill $PID_A $PID_B 2>/dev/null || true
    rm -rf "$TMP_DIR"
}
trap cleanup EXIT

# Build daemon to local tmp directory
echo "Building daemon..."
go build -C "$DAEMON_DIR" -o "$TMP_DIR/psync-daemon" ./cmd/daemon

# Start signal server (assumes it's already running or start it here)
# For now, we assume signal server is running on :8080

# Start Peer A
echo "Starting Peer A..."
"$TMP_DIR/psync-daemon" -root "$DIR_A" -id "peer-a" -group "test-group" &
PID_A=$!

# Start Peer B
echo "Starting Peer B..."
"$TMP_DIR/psync-daemon" -root "$DIR_B" -id "peer-b" -group "test-group" &
PID_B=$!

# Wait for daemons to initialize
sleep 5

# Create a file on Peer A
echo "Creating test file on Peer A..."
echo "Hello from peer-a" > "$DIR_A/test-file.txt"

# Wait for sync
echo "Waiting for sync..."
sleep 5

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

sleep 5

if [ -f "$DIR_A/response.txt" ]; then
    echo "✓ Bidirectional sync works!"
else
    echo "✗ Bidirectional sync failed!"
    exit 1
fi

# Test file deletion propagation
echo "Testing file deletion propagation..."
echo "Deleting test-file.txt from Peer A..."
rm "$DIR_A/test-file.txt"

# Wait for deletion sync
echo "Waiting for deletion to sync..."
sleep 5

# Verify file is deleted on Peer B
echo "Verifying deletion on Peer B..."
if [ ! -f "$DIR_B/test-file.txt" ]; then
    echo "✓ File deletion propagated correctly!"
else
    echo "✗ File deletion failed to propagate!"
    echo "File still exists on Peer B:"
    ls -la "$DIR_B"
    exit 1
fi

# Test deletion from Peer B to Peer A
echo "Creating another file to test deletion from Peer B..."
echo "To be deleted" > "$DIR_B/delete-me.txt"

sleep 5

if [ -f "$DIR_A/delete-me.txt" ]; then
    echo "✓ File created for deletion test"
else
    echo "✗ Failed to create file for deletion test"
    exit 1
fi

echo "Deleting delete-me.txt from Peer B..."
rm "$DIR_B/delete-me.txt"

sleep 5

if [ ! -f "$DIR_A/delete-me.txt" ]; then
    echo "✓ Deletion from Peer B to Peer A works!"
else
    echo "✗ Deletion from Peer B to Peer A failed!"
    exit 1
fi

echo ""
echo "=== All integration tests passed! ==="
