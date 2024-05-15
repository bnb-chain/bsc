import { ethers } from "ethers";
import program from "commander";


// Usage:
//   nvm use 20
//   node check_blobtx.js --rpc https://data-seed-prebsc-1-s1.binance.org:8545 --startNum 40345993
//   node check_blobtx.js --rpc https://data-seed-prebsc-1-s1.binance.org:8545 --startNum 40345993 --endNum 40345994
program.option("--rpc <Rpc>", "Rpc Server URL");
program.option("--startNum <Num>", "start block", 0);
program.option("--endNum <Num>", "end block", 0);
program.parse(process.argv);

const provider = new ethers.JsonRpcProvider(program.rpc);
const main = async () => {
    var startBlock = parseInt(program.startNum)
    var endBlock = parseInt(program.endNum)
    if (isNaN(endBlock) || isNaN(startBlock) || startBlock == 0) {
        console.error("invalid input, --startNum", program.startNum, "--end", program.endNum)
        return
    }
    // if --endNum is not specified, set it to the latest block number.
    if (endBlock == 0) {
        endBlock = await provider.getBlockNumber();
    }
    if (startBlock > endBlock) {
        console.error("invalid input, startBlock:",startBlock, " endBlock:", endBlock);
        return
    }

    for (let i = startBlock; i <= endBlock; i++) {
        console.log("startBlock:",startBlock, "endBlock:", endBlock, "curBlock", i);
        let blockData = await provider.getBlock(i);
        for  (let txIndex = 0; txIndex<= blockData.transactions.length - 1; txIndex++) {
            let txHash = blockData.transactions[txIndex]
            let txData =  await provider.getTransaction(txHash);
            if (txData.type == 3) {
                console.log("block:",i, " txIndex:", txIndex, " txHash:", txHash, "txType(BlobTx):", txData.type);
            }
        }
    }
};
main().then(() => process.exit(0))
    .catch((error) => {
        console.error(error);
        process.exit(1);
    });