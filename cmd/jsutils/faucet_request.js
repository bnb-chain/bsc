import { ethers } from "ethers";
import program from "commander";

// depends on ethjs v6.11.0+ for 4844, https://github.com/ethers-io/ethers.js/releases/tag/v6.11.0
// Usage:
//   nvm use 20
//   node faucet_request.js --rpc https://data-seed-prebsc-1-s1.binance.org:8545 --startNum 39539137
//   node faucet_request.js --rpc https://data-seed-prebsc-1-s1.binance.org:8545 --startNum 39539137 --endNum 40345994
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

    let startBalance = await provider.getBalance("0xaa25Aa7a19f9c426E07dee59b12f944f4d9f1DD3", startBlock)
    let endBalance = await provider.getBalance("0xaa25Aa7a19f9c426E07dee59b12f944f4d9f1DD3", endBlock)
    let numFaucetRequest = (startBalance - endBalance) / 0.3
    console.log("successful faucet request: ",numFaucetRequest);
};
main().then(() => process.exit(0))
    .catch((error) => {
        console.error(error);
        process.exit(1);
    });
