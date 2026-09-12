#!/bin/sh
set -eu

if ! getent group bazusop >/dev/null 2>&1; then
  groupadd --system bazusop
fi
if ! id bazusop >/dev/null 2>&1; then
  useradd --system --gid bazusop --home-dir /nonexistent --shell /usr/sbin/nologin bazusop
fi
chown root:bazusop /etc/bazusop
chmod 0750 /etc/bazusop

if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload
  systemctl enable bazusop-hub.service
  if systemctl is-active --quiet bazusop-hub.service; then
    systemctl try-restart bazusop-hub.service
  fi
fi
