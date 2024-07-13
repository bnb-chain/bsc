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

const stakeHubAbi = [
    "function getValidatorDescription(address validatorAddr) external view returns (string[])"
]
const addrValidatorSet = '0x0000000000000000000000000000000000001000';
const addrSlash = '0x0000000000000000000000000000000000001001';
const validatorSet = new ethers.Contract(addrValidatorSet, validatorSetAbi, provider);
const slashIndicator = new ethers.Contract(addrSlash, slashAbi,  provider)

const addrStakeHub = '0x0000000000000000000000000000000000002002';
const stakeHub = new ethers.Contract(addrStakeHub, stakeHubAbi, provider)
// const validatorMap = new Map([
//     ["0x9f1b7fae54be07f4fee34eb1aacb39a1f7b6fc92", "TWStaking"],
//     ["0x5009317fd4f6f8feea9dae41e5f0a4737bb7a7d5", "NodeReal"],
//     ["0x78f3aDfC719C99674c072166708589033e2d9afe", "nariox"],
//     ["0x2465176C461AfB316ebc773C61fAEe85A6515DAA", "TW Staking"],
// ])


const main = async () => {
    let blockNum = ethers.getNumber(program.Num)
    if (blockNum === 0) {
       blockNum = await provider.getBlockNumber()
    }
    let block = await provider.getBlock(blockNum)
    console.log("At block", blockNum, "time", block.date)
    const data = await validatorSet.getLivingValidators({blockTag:blockNum})
    // let totalSlash = 0
    for (let i = 0; i < data[0].length; i++) {
        let addr = data[0][i];
        console.log(addr)
        let value = await stakeHub.getValidatorDescription(addr)
        console.log( addr,  value)
        // let info = await slashIndicator.getSlashIndicator(addr, {blockTag:blockNum})
        // let count = ethers.toNumber(info[1])
        // totalSlash += count
        // console.log("index:", i, "address:", addr, "slashes:", number)
    }
    // console.log("Total slash count", totalSlash)
};
main().then(() => process.exit(0))
    .catch((error) => {
        console.error(error);
        process.exit(1);
    });