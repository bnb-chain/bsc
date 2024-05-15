import { ethers } from "ethers";
import program from "commander";


// Usage:
//   nvm use 20
//   node check_blob_info.js --Rpc https://data-seed-prebsc-1-s1.binance.org:8545 --StartBlock 40345993 --EndBlock 40345993
program.option("--Rpc <Rpc>", "Rpc Server URL");

// For blob test, set the default variable same as node-deploy repo:
// https://github.com/bnb-chain/node-deploy/blob/main/.env
//   MinBlocksForBlobRequests=576
//   DefaultExtraReserveForBlobRequests=32
program.option("--MinBlocksForBlobRequests <Num>", "MinBlocksForBlobRequests Num", 576);
program.option("--DefaultExtraReserveForBlobRequests <Num>", "DefaultExtraReserveForBlobRequests Num", 32);
// --CheckBlockSize <Num>: the number of block's blob to be checked
//   default: 0, the range will be: (curBlockNum - <DefaultExtraReserveForBlobRequests> - <MinBlocksForBlobRequests> + 1) -> curBlockNum
//   others:     the range will be: (curBlockNum - <CheckBlockSize> + 1) -> curBlockNum, default 0:
program.option("--CheckBlockSize <Num>", "the number of blocks&blob to be check", 0);
program.option("--StartBlock <Num>", "start from this block", 0);
program.option("--EndBlock <Num>", "end by this block", 0);
program.parse(process.argv);

const provider = new ethers.JsonRpcProvider(program.Rpc);
const minBlobBlocks = program.MinBlocksForBlobRequests;
const extraReserve = program.DefaultExtraReserveForBlobRequests
var checkBlockSize = program.CheckBlockSize
var startBlock = parseInt(program.StartBlock)
var endBlock = parseInt(program.EndBlock)

// |-- ...--|-- <DefaultExtraReserveForBlobRequests> -- | -- <MinBlocksForBlobRequests> --|
const main = async () => {
    const curBlockNum = await provider.getBlockNumber();
    if (checkBlockSize == 0) {
        // default will be extraReserve + minBlobBlocks
        checkBlockSize = extraReserve + minBlobBlocks
    }
    if (checkBlockSize > curBlockNum) {
        checkBlockSize = curBlockNum
    }
    const startBlockNum = curBlockNum - checkBlockSize + 1
    startBlock = startBlock + 0
    endBlock = endBlock + 0
    console.log("startBlockNum:",startBlock, " curBlockNum:", endBlock);
    for (let i = startBlock; i <= endBlock; i++) {
        // const block = await provider.getBlock(i);
        let blockData = await provider.getBlock(i);
        for  (let txIndex = 0; txIndex<= blockData.transactions.length - 1; txIndex++) {
            let txHash = blockData.transactions[txIndex]
            let txData =  await provider.getTransaction(txHash);
            console.log("block:",i, " txIndex:", txIndex, " txHash:", txHash, "txType:", txData.type);
            // if (txData.type != 3) {
            //     continue
            // }
            // it is a BlobTx
            // lastGasPrice = txData.gasPrice
            // break
        }
        // console.log(blockData.miner, "version =", major + "." + minor + "." + patch, " MinGasPrice = " + lastGasPrice)
    }
};
main().then(() => process.exit(0))
    .catch((error) => {
        console.error(error);
        process.exit(1);
    });