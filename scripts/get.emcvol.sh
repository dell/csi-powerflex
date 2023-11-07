#!/bin/sh
ls -l /dev/disk/by-id | grep " emc-vol-$1 " | awk '/emc-vol/ { gsub(".*emc-vol-", ""); gsub(".*/", ""); print $0}'
