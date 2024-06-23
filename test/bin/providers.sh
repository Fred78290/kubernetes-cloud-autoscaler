echo "Run providers test"

export Test_AuthMethodKey=NO
export Test_Sudo=NO
export Test_CIDR=YES
export Test_createVM=YES
export Test_getVM=YES
export Test_statusVM=YES
export Test_powerOnVM=YES
export Test_powerOffVM=YES
export Test_shutdownGuest=YES
export Test_deleteVM=YES

go test $VERBOSE --run Test_Provider -timeout 1200s -count 1 -race ./providers
