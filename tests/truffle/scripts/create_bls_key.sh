#!/usr/bin/expect
# 6 num wanted
set wallet_password 123456 
# 10 characters at least wanted
set account_password 1234567890

set timeout 5
spawn geth bls account new --datadir [lindex $argv 0]
expect "*assword:*"
send "$wallet_password\r"
expect "*assword:*"
send "$wallet_password\r"
expect "*assword:*"
send "$account_password\r"
expect "*assword:*"
send "$account_password\r"
expect EOF