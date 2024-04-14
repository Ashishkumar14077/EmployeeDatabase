#! /bin/bash

set -x

SCRIPTPATH=$(cd "$(dirname "$0")"; pwd -P)

export GOPATH="$SCRIPTPATH"/../../../../../../common/goCommon/:"$SCRIPTPATH"/../../../../../../
echo $GOPATH
export ENGINE_GOPATH="$SCRIPTPATH"/../../../../../../
export GO111MODULE="off"
type go &>/dev/null
if (( $? != 0 ))
then
        echo "Installing golang"
        sudo yum install -y golang
        if (( $? != 0 ))
        then
                sudo apt-get install -y golang
        fi
fi

cd "$SCRIPTPATH"

if [ -f "./bin/main_engine" ] ; then
        make clean
fi

make || { echo "Build failed"; exit 1; }
"$SCRIPTPATH"/tools/install_engine_pre_req.sh || { echo "Build failed"; exit 1; }
