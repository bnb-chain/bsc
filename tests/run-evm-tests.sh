#!/usr/bin/env bash

cd ..
git apply tests/0001-diff-go-ethereum.patch
cd tests
go test -run . -v >test.log
PASS=`cat test.log |grep "PASS:" |wc -l`
cat test.log|grep FAIL > fail.log
FAIL=`cat fail.log |grep "FAIL:" |wc -l`
echo "PASS",$PASS,"FAIL",$FAIL
if [ $FAIL -ne 0 ]
then
    cat fail.log
    exit 1
fi
