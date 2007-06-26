#!/bin/bash

DIR=/data2/extract
HOSTS=$*
test -z "$HOSTS" && HOSTS=$ALL
FILELIST=/var/cache/rpm-find-changes/etc.txt

die() {
  echo $*
  exit 1
}

for h in $HOSTS; do
  echo $h
  mkdir -p $DIR/$h
  rsync -a --copy-unsafe-links --quiet --files-from=:$FILELIST $h:/ $DIR/$h
done

