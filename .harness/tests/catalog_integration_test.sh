#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
workspace="$(mktemp -d)"
trap 'rm -rf "$workspace"' EXIT

data="$workspace/data"

# Harness skill with a folded (>) block-scalar description, matching the shape
# ponytail's and writing-skills' SKILL.md frontmatter actually use.
mkdir -p "$data/skills/fixture-block-scalar"
cat > "$data/skills/fixture-block-scalar/SKILL.md" <<'EOF'
---
name: fixture-block-scalar
description: >
  First indented line of the description.
  Second indented line of the description.
---
EOF

# build_catalog.py requires every managed root to exist; keep these minimal.
for source in agent-skills ponytail; do
  mkdir -p "$data/upstreams/$source/skills/fixture"
  printf '%s\n' '---' 'name: fixture' 'description: fixture' '---' > "$data/upstreams/$source/skills/fixture/SKILL.md"
done
mkdir -p "$data/upstreams/writing-skills/for-agents/fixture"
printf '%s\n' '---' 'name: fixture' 'description: fixture' '---' > "$data/upstreams/writing-skills/for-agents/fixture/SKILL.md"
mkdir -p "$data/upstreams/agency-agents/engineering"
printf '%s\n' '# Fixture engineer' > "$data/upstreams/agency-agents/engineering/fixture.md"

# Simulate the image build: scanning happens under the staging `data` dir, but
# HARNESS_CATALOG_ROOT_UPSTREAMS names the runtime path the server will read from.
ARDVI_CATALOG_DATA_DIR="$data" HARNESS_CATALOG_ROOT_UPSTREAMS=/opt/ardvi/upstreams \
  python3 "$repo_root/.harness/scripts/build_catalog.py" >/dev/null

catalog="$data/catalog.json"

# (a) block-scalar description is joined into one line, not left as the literal ">".
grep -Fq '"First indented line of the description. Second indented line of the description."' "$catalog"

# (b) the harness skill root is the runtime path, not the build-time staging path.
grep -Fq '"root": "/opt/ardvi/skills/fixture-block-scalar"' "$catalog"
if grep -Fq "$workspace" "$catalog"; then
  echo "catalog leaks the build-time staging directory" >&2
  exit 1
fi

echo "catalog integration: PASS"
