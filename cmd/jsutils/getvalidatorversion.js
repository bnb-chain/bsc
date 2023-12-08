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
        let major = ethers.toNumber(ethers.dataSlice(blockData.extraData, 2, 3))
        let minor = ethers.toNumber(ethers.dataSlice(blockData.extraData, 3, 4))
        let patch = ethers.toNumber(ethers.dataSlice(blockData.extraData, 4, 5))
        console.log(blockData.miner, "version =", major + "." + minor + "." + patch)
    }
};
main().then(() => process.exit(0))
    .catch((error) => {
        console.error(error);
        process.exit(1);
    });