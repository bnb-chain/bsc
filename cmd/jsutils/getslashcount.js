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
    "function getValidatorDescription(address validatorAddr) external view returns (tuple(string, string, string, string))",
    "function consensusToOperator(address consensusAddr) public view returns (address)"
]
const addrValidatorSet = '0x0000000000000000000000000000000000001000';
const validatorSet = new ethers.Contract(addrValidatorSet, validatorSetAbi, provider);

const addrSlash = '0x0000000000000000000000000000000000001001';
const slashIndicator = new ethers.Contract(addrSlash, slashAbi,  provider)

const addrStakeHub = '0x0000000000000000000000000000000000002002';
const stakeHub = new ethers.Contract(addrStakeHub, stakeHubAbi, provider)

const validatorMap = new Map([
    //BSC
    ["0x37e9627A91DD13e453246856D58797Ad6583D762", "LegendII"],
    ["0xB4647b856CB9C3856d559C885Bed8B43e0846a47", "CertiK"],
    ["0x75B851a27D7101438F45fce31816501193239A83", "Figment"],
    ["0x502aECFE253E6AA0e8D2A06E12438FFeD0Fe16a0", "BscScan"],
    ["0xCa503a7eD99eca485da2E875aedf7758472c378C", "InfStones"],
    ["0x5009317FD4F6F8FeEa9dAe41E5F0a4737BB7A7D5", "NodeReal"],
    ["0x1cFDBd2dFf70C6e2e30df5012726F87731F38164", "Tranchess"],
    ["0xF8de5e61322302b2c6e0a525cC842F10332811bf", "Namelix"],
    ["0xCcB42A9b8d6C46468900527Bc741938E78AB4577", "Turing"],
    ["0x9f1b7FAE54BE07F4FEE34Eb1aaCb39A1F7B6FC92", "TWStaking"],
    ["0x7E1FdF03Eb3aC35BF0256694D7fBe6B6d7b3E0c8","LegendIII"],
    ["0x7b501c7944185130DD4aD73293e8Aa84eFfDcee7","MathW"],
    ["0x58567F7A51a58708C8B40ec592A38bA64C0697De","Legend"],
    ["0x460A252B4fEEFA821d3351731220627D7B7d1F3d","Defibit"],
    ["0x8A239732871AdC8829EA2f47e94087C5FBad47b6","The48Club"],
    ["0xD3b0d838cCCEAe7ebF1781D11D1bB741DB7Fe1A7","BNBEve"],
    ["0xF8B99643fAfC79d9404DE68E48C4D49a3936f787","Avengers"],
    ["0x4e5acf9684652BEa56F2f01b7101a225Ee33d23f","HashKey"],
    ["0x9bb56C2B4DBE5a06d79911C9899B6f817696ACFc","Feynman"],
    ["0xbdcc079BBb23C1D9a6F36AA31309676C258aBAC7","Fuji"],
    ["0x38944092685a336CB6B9ea58836436709a2adC89","Shannon"],
    ["0xfC1004C0f296Ec3Df4F6762E9EabfcF20EB304a2","Aoraki"],
    ["0xa0884bb00E5F23fE2427f0E5eC9E51F812848563","Coda"],
    ["0xe7776De78740f28a96412eE5cbbB8f90896b11A5","Ankr"],
    ["0xA2D969E82524001Cb6a2357dBF5922B04aD2FCD8","Pexmons"],
    ["0x5cf810AB8C718ac065b45f892A5BAdAB2B2946B9","Zen"],
    ["0x4d15D9BCd0c2f33E7510c0de8b42697CA558234a","LegendVII"],
    ["0x1579ca96EBd49A0B173f86C372436ab1AD393380","LegendV"],
    ["0xd1F72d433f362922f6565FC77c25e095B29141c8","LegendVI"],
    ["0xf9814D93b4d904AaA855cBD4266D6Eb0Ec1Aa478","Legend8"],
    ["0x025a4e09Ea947b8d695f53ddFDD48ddB8F9B06b7","Ciscox"],
    ["0xE9436F6F30b4B01b57F2780B2898f3820EbD7B98","LegendIV"],
    ["0xC2d534F079444E6E7Ff9DabB3FD8a26c607932c8","Axion"],
    ["0x9F7110Ba7EdFda83Fc71BeA6BA3c0591117b440D","LegendIX"],
    ["0xB997Bf1E3b96919fBA592c1F61CE507E165Ec030","Seoraksan"],
    ["0x286C1b674d48cFF67b4096b6c1dc22e769581E91","Sigm8"],
    ["0x73A26778ef9509a6E94b55310eE7233795a9EB25","Coinlix"],
    ["0x18c44f4FBEde9826C7f257d500A65a3D5A8edebc","Nozti"],
    ["0xA100FCd08cE722Dc68Ddc3b54237070Cb186f118","Tiollo"],
    ["0x0F28847cfdbf7508B13Ebb9cEb94B2f1B32E9503","Raptas"],
    ["0xfD85346c8C991baC16b9c9157e6bdfDACE1cD7d7","Glorin"],
    ["0x978F05CED39A4EaFa6E8FD045Fe2dd6Da836c7DF","NovaX"],
    ["0xd849d1dF66bFF1c2739B4399425755C2E0fAbbAb","Nexa"],
    ["0xA015d9e9206859c13201BB3D6B324d6634276534","Star"],
    ["0x5ADde0151BfAB27f329e5112c1AeDeed7f0D3692","Veri"],
    ["0xd6Ab358AD430F65EB4Aa5a1598FF2c34489dcfdE","Saturn"],
    //Chapel
    ["0x08265dA01E1A65d62b903c7B34c08cB389bF3D99","Ararat"],
    ["0x7f5f2cF1aec83bF0c74DF566a41aa7ed65EA84Ea","Kita"],
    ["0x53387F3321FD69d1E030BB921230dFb188826AFF","Fuji"],
    ["0x76D76ee8823dE52A1A431884c2ca930C5e72bff3","Seoraksan"],
    ["0xd447b49CD040D20BC21e49ffEa6487F5638e4346","Everest"],
    ["0x1a3d9D7A717D64e6088aC937d5aAcDD3E20ca963","Elbrus"],
    ["0x40D3256EB0BaBE89f0ea54EDAa398513136612f5","Bloxroute"],
    ["0xF9a1Db0d6f22Bd78ffAECCbc8F47c83Df9FBdbCf","Test"]
]);


const main = async () => {
    let blockNum = ethers.getNumber(program.Num)
    if (blockNum === 0) {
       blockNum = await provider.getBlockNumber()
    }
    let block = await provider.getBlock(blockNum)
    console.log("At block", blockNum, "time", block.date)
    const data = await validatorSet.getLivingValidators({blockTag:blockNum})
    let totalSlash = 0
    for (let i = 0; i < data[0].length; i++) {
        let addr = data[0][i];
        var val
        if (!validatorMap.has(addr)) {
            let opAddr = await stakeHub.consensusToOperator(addr, {blockTag:blockNum})
            let value = await stakeHub.getValidatorDescription(opAddr, {blockTag:blockNum})
            val = value[0]
            console.log(addr, val)
        } else {
            val = validatorMap.get(addr)
        }
        let info = await slashIndicator.getSlashIndicator(addr, {blockTag:blockNum})
        let count = ethers.toNumber(info[1])
        totalSlash += count
        console.log("address:", addr, val, count)
    }
    console.log("Total slash count", totalSlash)
};
main().then(() => process.exit(0))
    .catch((error) => {
        console.error(error);
        process.exit(1);
    });