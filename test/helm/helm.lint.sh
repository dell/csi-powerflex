#!/bin/sh
FILES="10vols 2vols 7vols xfspre"

for i in $FILES;
do
helm lint -n helmtest-vxflexos $i
done
