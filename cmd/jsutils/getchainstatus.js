import { ethers } from "ethers";
import program from "commander";

program.option("--rpc <rpc>", "Rpc");
program.option("--startNum <startNum>", "start num")
program.option("--endNum <endNum>", "end num")
program.option("--miner <miner>", "miner", "")
program.option("--num <Num>", "validator num", 21)

program.parse(process.argv);

const provider = new ethers.JsonRpcProvider(program.rpc)

function printUsage() {
    console.log("Usage:");
    console.log("  node getchainstatus.js --help");
    console.log("  node getchainstatus.js [subcommand] [options]");
    console.log("\nSubcommands:");
    console.log("  GetTxCount: find the block with max tx size of a range");
    console.log("  GetValidatorVersion: dump validators' binary version, based on Header.Extra");
    console.log("\nOptions:");
    console.log("  --rpc       specify the url of RPC endpoint");
    console.log("  --startNum  the start block number, for command GetTxCount");
    console.log("  --endNum    the end block number, for command GetTxCount");
    console.log("  --miner     the miner address, for command GetTxCount");
    console.log("  --num       the number of blocks to be checked, for command GetValidatorVersion");
    console.log("\nExample:");
    console.log("  node getchainstatus.js GetTxCount --rpc https://bsc-mainnet.nodereal.io/v1/454e504917db4f82b756bd0cf6317dce --startNum 40000001  --endNum 40000005")
    console.log("  node getchainstatus.js GetValidatorVersion --rpc https://bsc-mainnet.nodereal.io/v1/454e504917db4f82b756bd0cf6317dce --num 21")
}

// 1.cmd: "GetTxCount", usage:
// node getchainstatus.js GetTxCount --rpc https://bsc-mainnet.nodereal.io/v1/454e504917db4f82b756bd0cf6317dce --startNum 40000001  --endNum 40000005
// --miner:
//   specified: find the max txCounter from the specified validator
//   not specified: find the max txCounter from all validators
async function getTxCount()  {
    let txCount = 0;
    let num = 0;
    console.log("Find the max txs count between", program.startNum, "and", program.endNum);
    for (let i = program.startNum; i < program.endNum; i++) {
        if (program.miner !== "") {
            let blockData = await provider.getBlock(Number(i))
            if (program.miner !== blockData.miner) {
                continue
            }
        }
        let x = await provider.send("eth_getBlockTransactionCountByNumber", [
            ethers.toQuantity(i)]);
        let a = ethers.toNumber(x)
        if (a > txCount) {
            num = i;
            txCount = a;
        }
    }
    console.log("BlockNum = ", num, "TxCount =", txCount);
}

// 2.cmd: "GetValidatorVersion", usage:
// node getchainstatus.js GetValidatorVersion \
//      --rpc https://bsc-mainnet.nodereal.io/v1/454e504917db4f82b756bd0cf6317dce \
//       --num(optional): defualt 21, the number of blocks that will be checked
async function getValidatorVersion()  {
    const blockNum = await provider.getBlockNumber();
    console.log(blockNum);
    for (let i = 0; i < program.num; i++) {
        let blockData = await provider.getBlock(blockNum - i);
        // 1.get Geth client version
        let major = ethers.toNumber(ethers.dataSlice(blockData.extraData, 2, 3))
        let minor = ethers.toNumber(ethers.dataSlice(blockData.extraData, 3, 4))
        let patch = ethers.toNumber(ethers.dataSlice(blockData.extraData, 4, 5))

        // 2.get minimum txGasPrice based on the last non-zero-gasprice transaction
        let lastGasPrice = 0
        for  (let txIndex = blockData.transactions.length - 1; txIndex >= 0; txIndex--) {
            let txHash = blockData.transactions[txIndex]
            let txData =  await provider.getTransaction(txHash);
            if (txData.gasPrice == 0) {
                continue
            }
            lastGasPrice = txData.gasPrice
            break
        }
        console.log(blockData.miner, "version =", major + "." + minor + "." + patch, " MinGasPrice = " + lastGasPrice)
    }
};

const main = async () => {
    if (process.argv.length <= 2) {
        console.error('invalid process.argv.length', process.argv.length);
        printUsage()
        return
    }
    const cmd =  process.argv[2]
    if (cmd === "--help") {
        printUsage()
        return
    }
    if (cmd === "GetTxCount") {
        await getTxCount()
    } else if (cmd === "GetValidatorVersion") {
        await getValidatorVersion()
    } else {
        console.log("unsupported cmd", cmd);
    }
}

main().then(() => process.exit(0))
    .catch((error) => {
        console.error(error);
        process.exit(1);
    });