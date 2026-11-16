#!/bin/sh
case "$1" in
  off) echo closed ;;
  empty) echo empty-selection ;;
  fail) echo failed-path ;;
  *) echo ok ;;
esac
