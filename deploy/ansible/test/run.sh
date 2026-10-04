#!/usr/bin/env bash
# Runs the role against a throwaway systemd container, twice, and checks that
# the second run changes nothing and the service is healthy.
# Needs: docker, ansible-core with the community.docker collection, bin/permwatch.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
ansible_dir="$(dirname "$here")"
name=permwatch-ansible-test
log="$(mktemp)"

cleanup() { docker rm -f "$name" >/dev/null 2>&1 || true; rm -f "$log"; }
trap cleanup EXIT

docker build -q -t "$name" "$here" >/dev/null
docker rm -f "$name" >/dev/null 2>&1 || true
docker run -d --name "$name" --privileged --cgroupns=host \
  -v /sys/fs/cgroup:/sys/fs/cgroup:rw --tmpfs /run --tmpfs /run/lock "$name" >/dev/null

run_playbook() {
  ANSIBLE_ROLES_PATH="$ansible_dir/roles" ANSIBLE_HOST_KEY_CHECKING=False \
  ansible-playbook -i "$name," -c community.docker.docker \
    -e ansible_python_interpreter=/usr/bin/python3 \
    -e permwatch_api_url=https://api.testnet.klever.org \
    -e permwatch_node_url=https://node.testnet.klever.org \
    -e @"$ansible_dir/group_vars.example.yml" \
    -e '{"ansible_become": false}' \
    "$here/playbook.yml" 2>>"$log"
}

echo "== first run"
run_playbook | tee -a "$log" | grep -E "^(TASK|ok|changed|failed|fatal|PLAY RECAP)|changed=" || true
echo "== second run (must change nothing)"
second="$(run_playbook)"
echo "$second" | grep -E "changed="
echo "$second" | grep -qE "changed=0 .*failed=0" || { echo "FAIL: second run was not idempotent"; exit 1; }

echo "== service state"
docker exec "$name" systemctl is-active permwatch
docker exec "$name" systemctl is-enabled permwatch
docker exec "$name" systemctl show -p User --value permwatch
docker exec "$name" python3 -c "import urllib.request; print('healthz', urllib.request.urlopen('http://127.0.0.1:8080/healthz').status)"
docker exec "$name" stat -c '%a %U:%G %n' /etc/permwatch/permwatch.env /var/lib/permwatch
echo "OK"
