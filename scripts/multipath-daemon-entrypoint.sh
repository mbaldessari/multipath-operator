#!/bin/bash
set -euo pipefail

# Multipath daemon entrypoint script
# This script initializes and manages the multipath daemon

log() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') [multipath-daemon] $*"
}

log "Starting multipath daemon container"

# Ensure the config file exists
MULTIPATH_CONF="/etc/multipath.conf"
CONFIG_SOURCE="/etc/multipath/multipath.conf"
BINDINGS_FILE="/etc/multipath/bindings"

if [[ -f "$CONFIG_SOURCE" ]]; then
    log "Using configuration from ConfigMap: $CONFIG_SOURCE"
    cp "$CONFIG_SOURCE" "$MULTIPATH_CONF"
else
    log "No configuration found in ConfigMap, using default"
    cat > "$MULTIPATH_CONF" << 'EOF'
# Default multipath configuration
defaults {
    user_friendly_names yes
    find_multipaths yes
    path_grouping_policy failover
}
EOF
fi

# Ensure bindings file exists (required by multipathd)
if [[ ! -f "$BINDINGS_FILE" ]]; then
    log "Creating multipath bindings file: $BINDINGS_FILE"
    touch "$BINDINGS_FILE"
fi

log "Multipath configuration:"
cat "$MULTIPATH_CONF"

# Load necessary kernel modules
log "Loading device-mapper modules"
modprobe dm_multipath || log "Warning: Could not load dm_multipath module"
modprobe dm_round_robin || log "Warning: Could not load dm_round_robin module"

# Create device nodes if they don't exist
log "Ensuring device nodes exist"
[[ ! -e /dev/mapper ]] && mkdir -p /dev/mapper
[[ ! -e /dev/mapper/control ]] && mknod /dev/mapper/control c 10 236 || true

# Start udev if not running (for device discovery)
if ! pgrep udevd > /dev/null; then
    log "Starting udev daemon"
    /lib/systemd/systemd-udevd --daemon || log "Warning: Could not start udevd"
fi

# Start multipathd
log "Starting multipathd daemon"
/sbin/multipathd -d -s &
MULTIPATHD_PID=$!

# Function to handle signals
cleanup() {
    log "Received signal, shutting down multipathd"
    if [[ -n "${MULTIPATHD_PID:-}" ]] && kill -0 "$MULTIPATHD_PID" 2>/dev/null; then
        kill "$MULTIPATHD_PID"
        wait "$MULTIPATHD_PID" 2>/dev/null || true
    fi
    log "Multipath daemon stopped"
    exit 0
}

# Set up signal handlers
trap cleanup SIGTERM SIGINT

# Health monitoring loop
log "Starting health monitoring loop"
while true; do
    sleep 30
    
    # Check if multipathd is still running
    if ! kill -0 "$MULTIPATHD_PID" 2>/dev/null; then
        log "ERROR: multipathd process died, restarting"
        /sbin/multipathd -d -s &
        MULTIPATHD_PID=$!
    fi
    
    # Log current multipath status
    if multipath -l > /dev/null 2>&1; then
        log "Multipath devices: $(multipath -l | grep -c '^[a-zA-Z0-9]' || echo '0')"
    else
        log "No multipath devices found or multipath command failed"
    fi
done