#!/usr/bin/env bash
cd ..
# Non-recursive on purpose: skip the nested LegacyTests submodule. Its
# Constantinople-era storage-collision fixtures (InitCollision,
# create2collisionStorage, dynamicAccountOverwriteEmpty) fail under
# go-ethereum's EIP-7610 allowlist collision check (v1.17.3) and are
# structurally impossible on BSC (EIP-158 active from genesis). Upstream
# go-ethereum likewise does not check out LegacyTests, so TestLegacyState
# self-skips with "missing test files". tests/testdata and
# tests/evm-benchmarks are top-level submodules and are still fetched.
git submodule update --init --depth 1
# 0001 only flips the Shanghai instruction-set base to Merge (upstream's base)
# for the standard state tests. The BSC-specific precompile removals it used to
# carry are now handled in-tree: core/vm selects the standard (…Eth) precompile
# sets when Parlia is not configured (rules.IsNotInBSC), so no patch is needed
# for those anymore.
git apply tests/0001-diff-go-ethereum.patch
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
