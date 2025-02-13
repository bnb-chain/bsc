#!/usr/bin/env bash
cd ..
git submodule update --init --depth 1 --recursive
git apply tests/0001-diff-go-ethereum.patch
cd tests
rm -rf spec-tests && mkdir spec-tests && cd spec-tests
wget https://github.com/ethereum/execution-spec-tests/releases/download/pectra-devnet-6%40v1.0.0/fixtures_pectra-devnet-6.tar.gz
tar xzf fixtures_pectra-devnet-6.tar.gz && rm -f fixtures_pectra-devnet-6.tar.gz
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
