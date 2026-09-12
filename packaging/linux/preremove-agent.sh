#!/bin/sh
set -eu

if command -v systemctl >/dev/null 2>&1; then
  systemctl stop bazusop-agent.service || true
  systemctl disable bazusop-agent.service || true
fi
