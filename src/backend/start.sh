#! /bin/bash

set -x

SCRIPTPATH=$(cd "$(dirname "$0")"; pwd -P)

[ ! -f "$SCRIPTPATH"/bin/backend ] && { echo "ENGINE not built yet"; exit 1; }

# [ -f "$SCRIPTPATH"/config/config.json ] && cp -bf "$SCRIPTPATH"/config/*.json "$SCRIPTPATH"/ && (sed -i s/127.0.0.1/$(hostname -I | tr -d ' ')/g "$SCRIPTPATH"/config.json; sed -i 's/\("instanceId"[[:blank:]]*:[[:blank:]]*\)"[^"]*\(.*\)/\1"'$(uuidgen)'\2/g' config.json) #To support config maps

# export ENGINE_GOPATH="$SCRIPTPATH"/../../

# export SSLKEYLOGFILE=~/ssl_key.txt

if [ ! -z $1 ]
then
    echo "Configuration File Name" $1
    "$SCRIPTPATH"/bin/backend $1
else
    "$SCRIPTPATH"/bin/backend
fi
