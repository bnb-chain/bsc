#!/usr/bin/env bash

echo "0. prepare---------------------------------------------------------------------------------\n\n"
echo 123abc >bls-password.txt
echo 123abc7890 > bls-account-password.txt
basedir=$(cd `dirname $0`; pwd)
workspace=${basedir}/../../../

echo "1. create a bls account--------------------------------------------------------------------\n\n"
${workspace}/build/bin/geth bls account new --blsaccountpassword ./bls-account-password.txt --blspassword ./bls-password.txt --datadir ./bls
${workspace}/build/bin/geth bls account list  --blspassword ./bls-password.txt --datadir ./bls

echo "2. import a bls account by passing file including a private key-----------------------------\n\n"
secretKey=`${workspace}/build/bin/geth bls account new --show-private-key --blsaccountpassword ./bls-account-password.txt --blspassword ./bls-password.txt --datadir ./bls1 | grep private | awk '{print $NF}'`
echo ${secretKey} > ./bls1/secretKey
${workspace}/build/bin/geth bls account import  --blspassword ./bls-password.txt --datadir ./bls ./bls1/secretKey 
${workspace}/build/bin/geth bls account list  --blspassword ./bls-password.txt --datadir ./bls

echo "3. delete the imported account above--------------------------------------------------------\n\n"
publicKey=`${workspace}/build/bin/geth bls account list  --blspassword ./bls-password.txt --datadir ./bls |grep public | tail -1 | awk '{print $NF}'`
${workspace}/build/bin/geth bls account delete  --blspassword ./bls-password.txt --datadir ./bls ${publicKey}
${workspace}/build/bin/geth bls account list  --blspassword ./bls-password.txt --datadir ./bls

echo "4. import a bls account by passing a keystore file------------------------------------------\n\n"
keystoreFile=`ls bls1/bls/keystore`
${workspace}/build/bin/geth bls account import  --blsaccountpassword ./bls-account-password.txt --blspassword ./bls-password.txt --datadir ./bls ./bls1/bls/keystore/${keystoreFile} 
${workspace}/build/bin/geth bls account list  --blspassword ./bls-password.txt --datadir ./bls

echo "5. clearup----------------------------------------------------------------------------------\n\n"
rm -rf bls
rm -rf bls1
rm -rf bls-password.txt
rm -rf bls-account-password.txt