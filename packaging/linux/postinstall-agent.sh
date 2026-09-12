#!/bin/sh
set -eu

install -d -m 0750 -o root -g root /etc/bazusop
install -d -m 0700 -o root -g root /var/lib/bazusop-agent

if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload
  systemctl enable bazusop-agent.service
  if [ -s /etc/bazusop/agent.env ]; then
    systemctl restart bazusop-agent.service
  fi
fi
