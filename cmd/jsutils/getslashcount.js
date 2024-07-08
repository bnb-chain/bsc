import { ethers } from "ethers";
import program from "commander";

program.option("--Rpc <Rpc>", "Rpc");
program.option("--Num <Num>", "num", 0)
program.parse(process.argv);

const provider = new ethers.JsonRpcProvider(program.Rpc);

const slashAbi = [
    "function getSlashIndicator(address validatorAddr) external view returns (uint256, uint256)"
]
const validatorSetAbi = [
    "function getLivingValidators() external view returns (address[], bytes[])"
]
const addrValidatorSet = '0x0000000000000000000000000000000000001000';
const addrSlash = '0x0000000000000000000000000000000000001001';
const validatorSet = new ethers.Contract(addrValidatorSet, validatorSetAbi, provider);
const slashIndicator = new ethers.Contract(addrSlash, slashAbi,  provider)


const main = async () => {
    let blockNum = ethers.getNumber(program.Num)
    if (blockNum === 0) {
       blockNum = await provider.getBlockNumber()
    }
    let block = await provider.getBlock(blockNum)
    console.log("current block", blockNum, "time", block.date)
    const data = await validatorSet.getLivingValidators({blockTag:blockNum})
    for (let i = 0; i < data[0].length; i++) {
        let addr = data[0][i];
        let info = await slashIndicator.getSlashIndicator(addr, {blockTag:blockNum})
        console.log("index:", i, "address:", addr, "slashes:", ethers.toNumber(info[1]))
    }
};
main().then(() => process.exit(0))
    .catch((error) => {
        console.error(error);
        process.exit(1);
    });