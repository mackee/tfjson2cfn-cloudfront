#!/usr/bin/env bash
# Regenerate the plan.json fixtures from each example's Terraform.
#
# The committed plan.json files are what the golden tests consume, so the test
# suite needs neither terraform nor network. Run this only when the Terraform
# changes. Requires terraform (see aqua.yaml) and network access for the AWS
# provider download.
#
#   examples/regenerate.sh
set -euo pipefail

cd "$(dirname "$0")/.."

# Share one provider download across all examples.
export TF_PLUGIN_CACHE_DIR="$PWD/.tf-plugin-cache"
export TF_IN_AUTOMATION=1
mkdir -p "$TF_PLUGIN_CACHE_DIR"

for dir in examples/*/; do
  [ -f "$dir/main.tf" ] || continue
  name="$(basename "$dir")"
  echo "==> $name"
  terraform -chdir="$dir" init -input=false -no-color >/dev/null
  terraform -chdir="$dir" plan -input=false -no-color -out=plan.tfplan >/dev/null
  # Drop the volatile "timestamp" field so the fixture only changes when the
  # Terraform does.
  terraform -chdir="$dir" show -json plan.tfplan \
    | python3 -c 'import json,sys; d=json.load(sys.stdin); d.pop("timestamp",None); json.dump(d,sys.stdout,separators=(",",":"))' \
    >"$dir/plan.json"
  rm -f "$dir/plan.tfplan"
  echo "    wrote $dir/plan.json"
done

echo "Done. Review the diff and run: go test ./..."
