#!/usr/bin/env bash
set -euo pipefail

echo "[guessit-ready] waiting for guessit service on 127.0.0.1:8000..."

for i in {1..20}; do
  if curl -sf http://127.0.0.1:8000/health >/dev/null 2>&1; then
    echo "[guessit-ready] guessit is ready"
    exit 0
  fi

  echo "[guessit-ready] not ready yet, retrying..."
  sleep 2
done

echo "[guessit-ready] ERROR: guessit never became ready" >&2
exit 1
