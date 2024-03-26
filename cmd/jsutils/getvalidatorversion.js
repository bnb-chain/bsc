import { ethers } from "ethers";
import program from "commander";

program.option("--Rpc <Rpc>", "Rpc");
program.option("--Num <Num>", "validator num", 21)
program.parse(process.argv);

const provider = new ethers.JsonRpcProvider(program.Rpc);

const main = async () => {
    const blockNum = await provider.getBlockNumber();
    console.log(blockNum);
    for (let i = 0; i < program.Num; i++) {
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
main().then(() => process.exit(0))
    .catch((error) => {
        console.error(error);
        process.exit(1);
    });