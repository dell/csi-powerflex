#!/bin/bash

if [ -z "$CSI_ENDPOINT" ]
then 
    echo "Warning: CSI_ENDPOINT is not set" 
else 
    socket_file=$CSI_ENDPOINT 
    if [[ $CSI_ENDPOINT == "unix://"* ]]
    then
        socket_file=$(echo $CSI_ENDPOINT | sed 's/^.\{7\}//')
    fi
    [ -e $socket_file ] && rm $socket_file
fi
exec "/csi-vxflexos"
