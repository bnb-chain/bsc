#!/usr/bin/env bash

echo "0. prepare---------------------------------------------------------------------------------"
echo 123abc7890 > bls-password.txt
echo 123abc7891 > bls-password1.txt
basedir=$(cd `dirname $0`; pwd)
workspace=${basedir}/../../../

echo "1. create a bls account--------------------------------------------------------------------"
${workspace}/build/bin/geth bls account new --blspassword ./bls-password.txt --datadir ./bls
${workspace}/build/bin/geth bls account list  --blspassword ./bls-password.txt --datadir ./bls

echo "2. import a bls account by passing file including a private key-----------------------------"
secretKey=`${workspace}/build/bin/geth bls account new --show-private-key --blspassword ./bls-password1.txt --datadir ./bls1 | grep private | awk '{print $NF}'`
echo ${secretKey} > ./bls1/secretKey
${workspace}/build/bin/geth bls account import  --blspassword ./bls-password.txt --datadir ./bls ./bls1/secretKey 
${workspace}/build/bin/geth bls account list  --blspassword ./bls-password.txt --datadir ./bls

echo "3. delete the imported account above--------------------------------------------------------"
publicKey=`${workspace}/build/bin/geth bls account list  --blspassword ./bls-password.txt --datadir ./bls |grep public | tail -1 | awk '{print $NF}'`
${workspace}/build/bin/geth bls account delete  --blspassword ./bls-password.txt --datadir ./bls ${publicKey}
${workspace}/build/bin/geth bls account list  --blspassword ./bls-password.txt --datadir ./bls

echo "4. import a bls account by passing a keystore file------------------------------------------"
keystoreFile=`ls bls1/bls/keystore`
${workspace}/build/bin/geth bls account import  --importedaccountpassword ./bls-password1.txt --blspassword ./bls-password.txt --datadir ./bls ./bls1/bls/keystore/${keystoreFile}
publicKey=`${workspace}/build/bin/geth bls account list  --blspassword ./bls-password.txt --datadir ./bls |grep public | tail -1 | awk '{print $NF}'` 

echo "5. generate ownership proof for the selected BLS account from the BLS wallet----------------"
${workspace}/build/bin/geth bls account generate-proof --blspassword ./bls-password.txt --datadir ./bls --chain-id 56 0x04d63aBCd2b9b1baa327f2Dda0f873F197ccd186 ${publicKey}

echo "6. clearup----------------------------------------------------------------------------------"
rm -rf bls
rm -rf bls1
rm -rf bls-password.txt
rm -rf bls-password1.txt