#!/usr/bin/env bash
# Copyright (C) 2023-2026 QuantumNous
#
# This program is free software: you can redistribute it and/or modify
# it under the terms of the GNU Affero General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.
set -euo pipefail

required_patterns=(
  '.env'
  '.env.*'
  '**/.env'
  '**/.env.*'
  '*.pem'
  '*.key'
  '*.p12'
  '*.pfx'
  '*.sql'
  '*.sqlite'
  '*.db'
  'backups'
  '**/backups'
)

for pattern in "${required_patterns[@]}"; do
  if ! grep -Fqx "$pattern" .dockerignore; then
    echo "✗ .dockerignore 缺少敏感构建上下文规则: $pattern" >&2
    exit 1
  fi
done

if ! grep -Fqx '!.env.example' .dockerignore; then
  echo '✗ .dockerignore 必须显式保留无密钥的 .env.example' >&2
  exit 1
fi

echo '✓ Docker 构建上下文已排除环境密钥、私钥、数据库与备份文件'
