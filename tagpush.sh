#!/bin/sh
if [ "$1" = "" ]; then echo need new version id; exit 2; fi
docker tag csi-vxflexos 10.247.98.98:5000/csi-vxflexos-rbo54:$1
docker push 10.247.98.98:5000/csi-vxflexos-rbo54:$1
docker images
