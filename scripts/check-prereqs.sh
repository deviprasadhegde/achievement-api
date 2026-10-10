#!/usr/bin/env bash
# check-prereqs.sh — verifies every tool needed for the Achievement API project.
# Usage (from the project root):  bash scripts/check-prereqs.sh
# Safe to run any number of times; it only reads, never changes anything.

set -u

PASS=0
FAIL=0
WARN=0

ok()   { printf "  ✅ %-16s %s\n" "$1" "$2"; PASS=$((PASS + 1)); }
bad()  { printf "  ❌ %-16s %s\n" "$1" "$2"; FAIL=$((FAIL + 1)); }
warn() { printf "  ⚠️  %-16s %s\n" "$1" "$2"; WARN=$((WARN + 1)); }

# check NAME "version command" "install hint"
check() {
  local name="$1" version_cmd="$2" hint="$3"
  if command -v "$name" >/dev/null 2>&1; then
    local v
    v=$(eval "$version_cmd" 2>/dev/null | head -n 1)
    ok "$name" "${v:-installed}"
  else
    bad "$name" "missing → $hint"
  fi
}

echo
echo "Machine: $(uname -s) $(uname -m)"
echo
echo "── Core ─────────────────────────────────────────────"
check brew          "brew --version"                 "see docs/01-prerequisites.md Step 1"
check git           "git --version"                  "brew install git"
check go            "go version"                     "brew install go"
check jq            "jq --version"                   "brew install jq"

echo "── Containers ───────────────────────────────────────"
check docker        "docker --version"               "brew install --cask docker"
if command -v docker >/dev/null 2>&1; then
  if docker info >/dev/null 2>&1; then
    ok "docker daemon" "running"
  else
    bad "docker daemon" "not running → open -a Docker"
  fi
fi

echo "── Cloud / Kubernetes / IaC ─────────────────────────"
check aws           "aws --version"                  "brew install awscli"
check kubectl       "kubectl version --client 2>/dev/null | head -n1" "brew install kubectl"
check helm          "helm version --short"           "brew install helm"
check terraform     "terraform version | head -n1"   "brew tap hashicorp/tap && brew install hashicorp/tap/terraform"

# Terraform must be >= 1.10 for native S3 state locking (use_lockfile)
if command -v terraform >/dev/null 2>&1; then
  tfv=$(terraform version | head -n1 | sed -E 's/^Terraform v([0-9]+)\.([0-9]+).*/\1 \2/')
  read -r tfmaj tfmin <<<"$tfv"
  if [ "${tfmaj:-0}" -gt 1 ] || { [ "${tfmaj:-0}" -eq 1 ] && [ "${tfmin:-0}" -ge 10 ]; }; then
    ok "terraform>=1.10" "yes"
  else
    bad "terraform>=1.10" "too old → brew uninstall terraform && brew install hashicorp/tap/terraform"
  fi
fi

echo "── Go quality / DB / cache tooling ──────────────────"
check golangci-lint "golangci-lint --version"        "brew install golangci-lint"
check govulncheck   "govulncheck -version 2>&1 | grep -i 'scanner' || echo installed" "go install golang.org/x/vuln/cmd/govulncheck@latest (and add ~/go/bin to PATH)"
check migrate       "migrate -version 2>&1"          "brew install golang-migrate"
check psql          "psql --version"                 "brew install libpq + add to PATH (Step 2a)"
check redis-cli     "redis-cli --version"            "brew install redis"

echo "── Security / load / GitHub ─────────────────────────"
check trivy         "trivy --version | head -n1"     "brew install trivy"
check hey           "echo installed"                 "brew install hey"
check gh            "gh --version | head -n1"        "brew install gh"

echo "── Accounts ─────────────────────────────────────────"
if command -v aws >/dev/null 2>&1; then
  if arn=$(aws sts get-caller-identity --query Arn --output text 2>/dev/null); then
    case "$arn" in
      *assumed-role/AWSReservedSSO_*) ok "aws login" "$arn" ;;
      *)                              warn "aws login" "$arn (expected an SSO assumed-role; see Step 5.5)";;
    esac
    region=$(aws configure get region 2>/dev/null)
    [ "$region" = "us-east-1" ] && ok "aws region" "$region" || warn "aws region" "${region:-unset} (guide assumes us-east-1)"
  else
    bad "aws login" "not logged in → aws sso login  (profile: ${AWS_PROFILE:-unset})"
  fi
fi
if command -v gh >/dev/null 2>&1; then
  if gh auth status >/dev/null 2>&1; then ok "github login" "authenticated"; else bad "github login" "gh auth login"; fi
fi
if git config --global user.email >/dev/null 2>&1; then
  ok "git identity" "$(git config --global user.name) <$(git config --global user.email)>"
else
  bad "git identity" "git config --global user.name/user.email (Step 6)"
fi

echo
echo "Passed: $PASS   Warnings: $WARN   Failed: $FAIL"
if [ "$FAIL" -eq 0 ]; then
  echo "All checks passed ✅"
  exit 0
else
  echo "Fix the ❌ items above, then run this script again."
  exit 1
fi
