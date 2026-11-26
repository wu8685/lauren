#!/bin/sh
# .lauren/runtime/red-replay 只给演示重放一次分类失败。文件在 gitignore 的 runtime 里，不改变候选。
if [ -f .lauren/runtime/red-replay ]; then
  echo lauren-class classification=behavior_fail
  exit 1
fi
./app/logic.sh off | grep -q closed
./app/logic.sh empty | grep -q empty-selection
./app/logic.sh fail | grep -q failed-path
