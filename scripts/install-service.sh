#!/usr/bin/env bash
set -euo pipefail

action="install"
install_dir=""
service_name="ice-art"
service_user="ice-art"
dry_run=0
purge=0
confirm_purge=""

usage() {
  cat <<'EOF'
Usage: install-service.sh [install|uninstall|start|stop|restart|status|dry-run] [--install-dir DIR] [--service-name NAME] [--user USER] [--purge --confirm-purge DIR]
EOF
}
if [[ $# -gt 0 && "$1" != --* ]]; then action="$1"; shift; fi
while [[ $# -gt 0 ]]; do
  case "$1" in
    --install-dir) install_dir="${2:?missing --install-dir value}"; shift 2 ;;
    --service-name) service_name="${2:?missing --service-name value}"; shift 2 ;;
    --user) service_user="${2:?missing --user value}"; shift 2 ;;
    --dry-run) dry_run=1; shift ;;
    --purge) purge=1; shift ;;
    --confirm-purge) confirm_purge="${2:?missing --confirm-purge value}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done
[[ "$service_name" =~ ^[A-Za-z0-9][A-Za-z0-9_.@-]{0,63}$ ]] || { echo "invalid service name" >&2; exit 2; }
[[ "$service_user" =~ ^[A-Za-z_][A-Za-z0-9_-]*\$?$ ]] || { echo "invalid service user" >&2; exit 2; }
[[ "$install_dir" != *$'\n'* && "$install_dir" != *$'\r'* ]] || { echo "invalid install directory" >&2; exit 2; }
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
if [[ -z "$install_dir" ]]; then install_dir="$(cd "$script_dir/.." && pwd)"; fi
[[ -d "$install_dir" ]] || { echo "install directory does not exist: $install_dir" >&2; exit 1; }
install_dir="$(cd "$install_dir" && pwd)"
escaped_install_dir="${install_dir//\\/\\\\}"
escaped_install_dir="${escaped_install_dir//\"/\\\"}"
escaped_install_dir="${escaped_install_dir//%/%%}"
directive_install_dir="${install_dir//\\/\\x5c}"
directive_install_dir="${directive_install_dir// /\\x20}"
directive_install_dir="${directive_install_dir//\"/\\x22}"
directive_install_dir="${directive_install_dir//#/\\x23}"
directive_install_dir="${directive_install_dir//;/\\x3b}"
directive_install_dir="${directive_install_dir//%/%%}"
unit_path="/etc/systemd/system/${service_name}.service"
update_unit_path="/etc/systemd/system/${service_name}-update.service"
update_path_path="/etc/systemd/system/${service_name}-update.path"
protected_root="/var/lib/ice-art-updater/${service_name}"
run() { if ((dry_run)); then printf '+ '; printf '%q ' "$@"; printf '\n'; else "$@"; fi; }
run_optional() { if ((dry_run)); then printf '+ '; printf '%q ' "$@"; printf '\n'; else "$@" || true; fi; }
unit_content="[Unit]
Description=Ice Art agent runtime
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${service_user}
WorkingDirectory=${directive_install_dir}
ExecStart=\"${escaped_install_dir}/ice_art\" -config \"${escaped_install_dir}/conf/app.yaml\" -runtime-mode systemd -service-name ${service_name}
Restart=on-failure
RestartSec=5s
TimeoutStopSec=35s

[Install]
WantedBy=multi-user.target"
update_unit_content="[Unit]
Description=Apply a verified Ice Art update
After=${service_name}.service
StartLimitIntervalSec=60
StartLimitBurst=3

[Service]
Type=oneshot
User=root
WorkingDirectory=${directive_install_dir}
ExecStart=\"${escaped_install_dir}/ice_art\" -config \"${escaped_install_dir}/conf/app.yaml\" -runtime-mode systemd -service-name ${service_name} -update-state-owner ${service_user} -update-protected-root ${protected_root} -update-helper-job \"${escaped_install_dir}/data/updates/pending-helper.json\"
Restart=on-failure
RestartSec=5s"
update_path_content="[Unit]
Description=Watch for a verified Ice Art update job

[Path]
PathExists=${directive_install_dir}/data/updates/pending-helper.json
Unit=${service_name}-update.service

[Install]
WantedBy=multi-user.target"
if [[ "$action" == "dry-run" ]]; then dry_run=1; action="install"; fi
case "$action" in
  install)
    [[ -x "$install_dir/ice_art" ]] || { echo "missing executable: $install_dir/ice_art" >&2; exit 1; }
    [[ -f "$install_dir/conf/app.yaml" ]] || { echo "missing config: $install_dir/conf/app.yaml" >&2; exit 1; }
    if ! id "$service_user" >/dev/null 2>&1; then
      run sudo useradd --system --home-dir "$install_dir" --shell /usr/sbin/nologin "$service_user"
    fi
    run sudo chown root:root "$install_dir" "$install_dir/ice_art" "$install_dir/conf"
    run sudo chmod 0755 "$install_dir" "$install_dir/ice_art" "$install_dir/conf"
    run sudo install -d -o "$service_user" -g "$service_user" -m 0750 "$install_dir/data" "$install_dir/logs" "$install_dir/workspace"
    run sudo install -d -o root -g root -m 0700 "$protected_root" "$protected_root/backups"
    if ((dry_run)); then echo "+ write ownership marker $install_dir/.ice-art-install-root"; else printf '%s\n' "$install_dir" | sudo tee "$install_dir/.ice-art-install-root" >/dev/null; sudo chmod 0600 "$install_dir/.ice-art-install-root"; fi
    run sudo install -d -m 0755 "$(dirname "$unit_path")"
    if ((dry_run)); then
      printf '%s\n--- %s ---\n%s\n--- %s ---\n%s\n' "$unit_content" "$update_unit_path" "$update_unit_content" "$update_path_path" "$update_path_content"
    else
      printf '%s\n' "$unit_content" | sudo tee "$unit_path" >/dev/null
      printf '%s\n' "$update_unit_content" | sudo tee "$update_unit_path" >/dev/null
      printf '%s\n' "$update_path_content" | sudo tee "$update_path_path" >/dev/null
    fi
    run sudo systemctl daemon-reload
    run sudo systemctl enable "$service_name"
    run sudo systemctl enable --now "${service_name}-update.path"
    run sudo systemctl restart "$service_name"
    ;;
  uninstall)
    run_optional sudo systemctl disable --now "$service_name" "${service_name}-update.path"
    run_optional sudo systemctl stop "${service_name}-update.service"
    run sudo rm -f "$unit_path"
    run sudo rm -f "$update_unit_path" "$update_path_path"
    run sudo systemctl daemon-reload
    if ((purge)); then
      [[ "$confirm_purge" == "$install_dir" && "$install_dir" != "/" ]] || { echo "purge confirmation must exactly match $install_dir" >&2; exit 2; }
      [[ -f "$install_dir/.ice-art-install-root" && "$(sudo cat "$install_dir/.ice-art-install-root")" == "$install_dir" ]] || { echo "install ownership marker is missing or invalid" >&2; exit 1; }
      run sudo rm -rf -- "$install_dir"
    fi
    ;;
  start|stop|restart|status) run sudo systemctl "$action" "$service_name" ;;
  *) echo "unknown action: $action" >&2; usage >&2; exit 2 ;;
esac
