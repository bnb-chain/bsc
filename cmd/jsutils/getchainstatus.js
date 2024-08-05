import { ethers } from "ethers";
import program from "commander";

// Global Options:
program.option("--rpc <rpc>", "Rpc");
// GetTxCount Options:
program.option("--startNum <startNum>", "start num")
program.option("--endNum <endNum>", "end num")
program.option("--miner <miner>", "miner", "")
// GetVersion Options:
program.option("--num <Num>", "validator num", 21)
// GetTopAddr Options:
program.option("--topNum <Num>", "top num of address to be displayed", 20)

program.parse(process.argv);

const provider = new ethers.JsonRpcProvider(program.rpc)

function printUsage() {
    console.log("Usage:");
    console.log("  node getchainstatus.js --help");
    console.log("  node getchainstatus.js [subcommand] [options]");
    console.log("\nSubcommands:");
    console.log("  GetTxCount: find the block with max tx size of a range");
    console.log("  GetVersion: dump validators' binary version, based on Header.Extra");
    console.log("  GetTopAddr: get hottest $topNum target address within a block range");
    console.log("\nOptions:");
    console.log("  --rpc       specify the url of RPC endpoint");
    console.log("  --startNum  the start block number, for command GetTxCount");
    console.log("  --endNum    the end block number, for command GetTxCount");
    console.log("  --miner     the miner address, for command GetTxCount");
    console.log("  --num       the number of blocks to be checked, for command GetVersion");
    console.log("  --topNum    the topNum of blocks to be checked, for command GetVersion");
    console.log("\nExample:");
    // mainnet https://bsc-mainnet.nodereal.io/v1/454e504917db4f82b756bd0cf6317dce
    console.log("  node getchainstatus.js GetTxCount --rpc https://bsc-testnet-dataseed.bnbchain.org --startNum 40000001  --endNum 40000005")
    console.log("  node getchainstatus.js GetVersion --rpc https://bsc-testnet-dataseed.bnbchain.org --num 21")
    console.log("  node getchainstatus.js GetTopAddr --rpc https://bsc-testnet-dataseed.bnbchain.org --startNum 40000001  --endNum 40000010 --topNum 10")
}

// 1.cmd: "GetTxCount", usage:
// node getchainstatus.js GetTxCount --rpc https://bsc-testnet-dataseed.bnbchain.org \
//      --startNum 40000001  --endNum 40000005 \
//      --miner(optional): specified: find the max txCounter from the specified validator,
//                         not specified: find the max txCounter from all validators
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

// 2.cmd: "GetVersion", usage:
// node getchainstatus.js GetVersion \
//      --rpc https://bsc-testnet-dataseed.bnbchain.org \
//       --num(optional): defualt 21, the number of blocks that will be checked
async function getBinaryVersion()  {
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

// 3.cmd: "GetTopAddr", usage:
// node getchainstatus.js GetTopAddr \
//      --rpc https://bsc-testnet-dataseed.bnbchain.org \
//      --startNum 40000001  --endNum 40000005 \
//      --topNum(optional): the top num of address to be displayed, default 20
function getTopKElements(map, k) {
    let entries = Array.from(map.entries());
    entries.sort((a, b) => b[1] - a[1]);
    return entries.slice(0, k);
}

async function getTopAddr()  {
    let countMap = new Map();
    console.log("Find the top target address, between", program.startNum, "and", program.endNum);
    for (let i = program.startNum; i < program.endNum; i++) {
        let blockData = await provider.getBlock(Number(i), true)
        for  (let txIndex = blockData.transactions.length - 1; txIndex >= 0; txIndex--) {
            let txData = await blockData.getTransaction(txIndex)
            if (txData.to == null) {
                console.log("Contract creation,txHash:", txData.hash)
                continue
            }
            let toAddr = txData.to;
            if (countMap.has(toAddr)) {
                countMap.set(toAddr, countMap.get(toAddr) + 1);
            } else {
                countMap.set(toAddr, 1);
            }
        }
        console.log("progress:", (program.endNum-i), "blocks left")
    }
    let tops = getTopKElements(countMap, program.topNum)
    tops.forEach((value, key) => {
        console.log(`${key}, Value: ${value}`);
      });
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
    } else if (cmd === "GetVersion") {
        await getBinaryVersion()
    } else if (cmd === "GetTopAddr") {
        await getTopAddr()
    } else {
        console.log("unsupported cmd", cmd);
        printUsage()
    }
}

main().then(() => process.exit(0))
    .catch((error) => {
        console.error(error);
        process.exit(1);
    });