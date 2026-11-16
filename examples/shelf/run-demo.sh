#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
BIN=${BIN:-$ROOT/dist/lauren}
if [ ! -x "$BIN" ]; then
  (cd "$ROOT" && go build -o "$BIN" ./cmd/lauren)
fi
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
cp -a "$ROOT/examples/shelf/." "$TMP/"
chmod +x "$TMP"/checks/*.sh "$TMP"/app/logic.sh
cd "$TMP"
git init -q -b main
git add .
git -c user.email=dev@example.com -c user.name=dev commit -qm init
"$BIN" change create --goal "空选择时走明确失败路径"
"$BIN" change submit --artifact artifacts/demand.json
"$BIN" change advance
"$BIN" change submit --artifact artifacts/plan.json
"$BIN" change advance
"$BIN" change submit --artifact artifacts/impl.json
"$BIN" check --name static
"$BIN" check --name unit
"$BIN" change advance
"$BIN" change submit --artifact artifacts/accept.json
"$BIN" check --name interact --environment integration
"$BIN" change accept --what delivery --source human --reason "对照关闭、空选择和失败路径看过" --scope "shelf 示例"
SHA=$(git rev-parse HEAD)
"$BIN" change integration record --sha "$SHA" --reason "示例假定已经合入 main，不自动 merge"
"$BIN" change advance
"$BIN" change submit --artifact artifacts/release.json
"$BIN" change accept --what release --source human --reason "声明发布这个集成版本" --scope "shelf 示例"
"$BIN" check --name version --environment staging
"$BIN" change advance
"$BIN" change problem add --summary "空选择曾没有失败路径" --repro "不传选择" --expected "明确失败" --actual "旧逻辑静默成功" --disposition later --reason "示例里的长期项，不挡住这次关闭" --defense "unit 检查" --scope project
"$BIN" change submit --artifact artifacts/retro.json
"$BIN" change advance
"$BIN" status
"$BIN" --json status
"$BIN" skills check
