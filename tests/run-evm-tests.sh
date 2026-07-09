#!/usr/bin/env bash
cd ..
git submodule update --init --depth 1 --recursive
git apply tests/0001-diff-go-ethereum.patch
git apply tests/0002-diff-go-ethereum.patch
cd tests
rm -rf spec-tests && mkdir spec-tests && cd spec-tests
wget https://github.com/ethereum/execution-spec-tests/releases/download/v5.1.0/fixtures_develop.tar.gz
tar xzf fixtures_develop.tar.gz && rm -f fixtures_develop.tar.gz
cd ..
go test -run . -v -short >test.log
PASS=`cat test.log |grep "PASS:" |wc -l`
cat test.log|grep FAIL > fail.log
FAIL=`cat fail.log |grep "FAIL:" |wc -l`
echo "PASS",$PASS,"FAIL",$FAIL
if [ $FAIL -ne 0 ]
then
    cat fail.log
    exit 1
fi
