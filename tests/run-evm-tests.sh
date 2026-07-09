#!/usr/bin/env bash
cd ..
# Non-recursive: skip the nested LegacyTests submodule, whose Constantinople
# storage-collision fixtures don't apply to BSC (EIP-158 from genesis) and are
# skipped by upstream too. Top-level submodules (testdata, evm-benchmarks) are
# still fetched.
git submodule update --init --depth 1
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
