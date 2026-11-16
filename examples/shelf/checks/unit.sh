#!/bin/sh
./app/logic.sh off | grep -q closed
./app/logic.sh empty | grep -q empty-selection
./app/logic.sh fail | grep -q failed-path
