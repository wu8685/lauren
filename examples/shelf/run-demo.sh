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
# 示例不写 red_gate，缺省是 required。runtime 标记不进候选指纹，用来重放一次分类失败，再跑真正的通过。
replay_red() {
  mkdir -p .lauren/runtime
  : > .lauren/runtime/red-replay
  set +e
  "$BIN" check --name unit
  code=$?
  set -e
  if [ "$code" -ne 4 ]; then
    echo "shelf demo: 预期 unit 先以 behavior_fail 失败，退出码是 $code" >&2
    exit 1
  fi
  rm -f .lauren/runtime/red-replay
  "$BIN" check --name unit
}

"$BIN" change create --goal "空选择时走明确失败路径"
"$BIN" change submit --artifact artifacts/demand.json
"$BIN" change advance
"$BIN" change submit --artifact artifacts/plan.json
"$BIN" change advance
"$BIN" change submit --artifact artifacts/impl.json
"$BIN" check --name static
replay_red
"$BIN" change advance
"$BIN" --json status | python3 -c 'import json,sys; red=json.load(sys.stdin)["red"]; assert red["required"] and red["recorded"] and not red["skipped"], red'
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
"$BIN" change problem add --title "空选择静默成功" --layer unit --symptom "未传选择" --repro "不传选择" --expected "明确失败" --actual "旧逻辑静默成功" --disposition later --reason "第一次只进入问题库" --scope project
python3 - "$TMP" <<'PY'
import json, pathlib, sys
root = pathlib.Path(sys.argv[1])
files = list((root / ".lauren" / "problems").glob("*.json"))
assert len(files) == 1, files
pb = json.loads(files[0].read_text())
assert pb["count"] == 1 and pb["status"] == "open", pb
proj = json.loads((root / "lauren.project.json").read_text())
assert all(c["name"] != "empty-selection" for c in proj["checks"])
PY
"$BIN" change submit --artifact artifacts/retro.json
"$BIN" change advance
"$BIN" change create --goal "同一问题再次出现" --release-stage excluded
"$BIN" change submit --artifact artifacts/demand.json
"$BIN" change advance
"$BIN" change submit --artifact artifacts/plan.json
"$BIN" change advance
"$BIN" change submit --artifact artifacts/impl.json
"$BIN" check --name static
replay_red
"$BIN" change advance
"$BIN" change submit --artifact artifacts/accept.json
"$BIN" check --name interact --environment integration
"$BIN" change accept --what delivery --source human --reason "再次看过失败路径" --scope "shelf 示例"
"$BIN" change advance
"$BIN" change problem add --title "空选择静默成功" --layer unit --symptom "未传选择" --target-check unit --disposition later --reason "第二次，登记到已有的 unit 检查" --scope project
python3 - "$TMP" <<'PY'
import json, pathlib, sys
root = pathlib.Path(sys.argv[1])
files = list((root / ".lauren" / "problems").glob("*.json"))
assert len(files) == 1, files
pb = json.loads(files[0].read_text())
assert pb["count"] == 2 and pb["status"] == "promoted" and pb["promoted_check"] == "unit", pb
assert pb["verified_fixed"] is False
proj = json.loads((root / "lauren.project.json").read_text())
assert [c["name"] for c in proj["checks"]] == ["static", "unit", "interact", "version"]
assert any(r["check"] == "unit" and r["claim"] == "空选择静默成功" for r in proj["requirements"])
assert not (root / "checks" / "promoted").exists()
PY
"$BIN" change submit --artifact artifacts/retro.json
"$BIN" change advance
"$BIN" status
"$BIN" --json status
"$BIN" skills check
