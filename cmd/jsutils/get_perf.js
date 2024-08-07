import { ethers } from "ethers";
import program from "commander";

program.option("--rpc <rpc>", "Rpc");
program.option("--startNum <startNum>", "start num")
program.option("--endNum <endNum>", "end num")
program.parse(process.argv);

const provider = new ethers.JsonRpcProvider(program.rpc)

const main = async () => {
    let txCountTotal = 0;
    let gasUsedTotal = 0;
    let inturnBlocks = 0;
    let justifiedBlocks = 0;
    let turnLength = await provider.send("parlia_getTurnLength", [
        ethers.toQuantity(program.startNum)]);
    for (let i = program.startNum; i < program.endNum; i++) {
        let txCount = await provider.send("eth_getBlockTransactionCountByNumber", [
            ethers.toQuantity(i)]);
        txCountTotal += ethers.toNumber(txCount)

        let header = await provider.send("eth_getHeaderByNumber", [
            ethers.toQuantity(i)]);
        let gasUsed = eval(eval(header.gasUsed).toString(10))
        gasUsedTotal += gasUsed
        let difficulty = eval(eval(header.difficulty).toString(10))
        if (difficulty == 2) {
            inturnBlocks += 1
        }
        let timestamp = eval(eval(header.timestamp).toString(10))

        let justifiedNumber = await provider.send("parlia_getJustifiedNumber", [
            ethers.toQuantity(i)]);
        if (justifiedNumber + 1 == i) {
            justifiedBlocks += 1
        } else {
            console.log("justified unexpected", "BlockNumber =", i,"justifiedNumber",justifiedNumber)    
        }
        console.log("BlockNumber =", i, "mod =", i%turnLength, "miner =", header.miner , "difficulty =", difficulty, "txCount =", ethers.toNumber(txCount), "gasUsed", gasUsed, "timestamp", timestamp)
    }

    let blockCount = program.endNum - program.startNum
    let txCountPerBlock = txCountTotal/blockCount

    let startHeader = await provider.send("eth_getHeaderByNumber", [
        ethers.toQuantity(program.startNum)]);
    let startTime = eval(eval(startHeader.timestamp).toString(10))
    let endHeader = await provider.send("eth_getHeaderByNumber", [
        ethers.toQuantity(program.endNum)]);
    let endTime = eval(eval(endHeader.timestamp).toString(10))
    let timeCost = endTime - startTime
    let avgBlockTime = timeCost/blockCount
    let inturnBlocksRatio = inturnBlocks/blockCount
    let justifiedBlocksRatio = justifiedBlocks/blockCount
    let tps = txCountTotal/timeCost
    let M = 1000000
    let avgGasUsedPerBlock = gasUsedTotal/blockCount/M
    let avgGasUsedPerSecond = gasUsedTotal/timeCost/M

    console.log("Get the performance between [", program.startNum, ",", program.endNum, ")");
    console.log("txCountPerBlock =", txCountPerBlock, "txCountTotal =", txCountTotal, "BlockCount =", blockCount, "avgBlockTime =", avgBlockTime, "inturnBlocksRatio =", inturnBlocksRatio, "justifiedBlocksRatio =", justifiedBlocksRatio);
    console.log("txCountPerSecond =", tps, "avgGasUsedPerBlock =", avgGasUsedPerBlock, "avgGasUsedPerSecond =", avgGasUsedPerSecond);
};

main().then(() => process.exit(0))
    .catch((error) => {
        console.error(error);
        process.exit(1);
    });