#!/bin/sh
# This script creates a partition for RAID md driver using fdisk.
if [ "$1"  = "" ]; then echo "need disk e.g. /dev/scinio"; exit 1; fi
echo "Running fdisk $1"
fdisk "$1" <<END
n
p
4


t
43
w
q
END
echo "Done"
exit 0
