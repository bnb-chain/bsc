import { ethers } from "ethers";
import program from "commander";

program.option("--rpc <rpc>", "Rpc");
program.option("--startNum <startNum>", "start num");
program.option("--endNum <endNum>", "end num");
program.option("--miner <miner>", "miner", "");
program.option("--num <Num>", "validator num", 21);
program.option("--turnLength <Num>", "the consecutive block length", 8);
program.option("--topNum <Num>", "top num of address to be displayed", 20);
program.option("--blockNum <Num>", "block num", 0);
program.option("--gasUsedThreshold <Num>", "gas used threshold", 5000000);
program.option("-h, --help", "");

function printUsage() {
    console.log("Usage:");
    console.log("  node getchainstatus.js --help");
    console.log("  node getchainstatus.js [subcommand] [options]");
    console.log("\nSubcommands:");
    console.log("  GetMaxTxCountInBlockRange: find the block with max tx size of a range");
    console.log("  GetBinaryVersion: dump validators' binary version, based on Header.Extra");
    console.log("  GetTopAddr: get hottest $topNum target address of a block range");
    console.log("  GetSlashCount: get slash state at a specific block height");
    console.log("  GetPerformanceData: analyze the performance data of a block range");
    console.log("  GetBlobTxs: get BlobTxs of a block range");
    console.log("  GetFaucetStatus: get faucet status of BSC testnet");
    console.log("  GetKeyParameters: dump some key governance parameter");
    console.log("  GetMevStatus: get mev blocks of a block range");
    console.log("  GetLargeTxs: get large txs of a block range");
    console.log("\nOptions:");
    console.log("  --rpc       specify the url of RPC endpoint");
    console.log("  --startNum  the start block number");
    console.log("  --endNum    the end block number");
    console.log("  --miner     the miner address");
    console.log("  --num       the number of blocks to be checked");
    console.log("  --topNum    the topNum of blocks to be checked");
    console.log("  --blockNum  the block number to be checked");
    console.log("\nExample:");
    // mainnet https://bsc-mainnet.nodereal.io/v1/454e504917db4f82b756bd0cf6317dce
    console.log("  node getchainstatus.js GetMaxTxCountInBlockRange --rpc https://bsc-testnet-dataseed.bnbchain.org --startNum 40000001  --endNum 40000005");
    console.log("  node getchainstatus.js GetBinaryVersion --rpc https://bsc-testnet-dataseed.bnbchain.org --num 21 --turnLength 4");
    console.log("  node getchainstatus.js GetTopAddr --rpc https://bsc-testnet-dataseed.bnbchain.org --startNum 40000001  --endNum 40000010 --topNum 10");
    console.log("  node getchainstatus.js GetSlashCount --rpc https://bsc-testnet-dataseed.bnbchain.org --blockNum 40000001"); // default: latest block
    console.log("  node getchainstatus.js GetPerformanceData --rpc https://bsc-testnet-dataseed.bnbchain.org --startNum 40000001  --endNum 40000010");
    console.log("  node getchainstatus.js GetBlobTxs --rpc https://bsc-testnet-dataseed.bnbchain.org --startNum 40000001  --endNum 40000010");
    console.log("  node getchainstatus.js GetFaucetStatus --rpc https://bsc-testnet-dataseed.bnbchain.org --startNum 40000001  --endNum 40000010");
    console.log("  node getchainstatus.js GetKeyParameters --rpc https://bsc-testnet-dataseed.bnbchain.org"); // default: latest block
    console.log("  node getchainstatus.js GetEip7623 --rpc https://bsc-testnet-dataseed.bnbchain.org --startNum 40000001  --endNum 40000010");
    console.log("  node getchainstatus.js GetMevStatus --rpc https://bsc-testnet-dataseed.bnbchain.org --startNum 40000001  --endNum 40000010");
    console.log("  node getchainstatus.js GetLargeTxs --rpc https://bsc-testnet-dataseed.bnbchain.org --startNum 40000001  --num 100 --gasUsedThreshold 5000000");
}

program.usage = printUsage;
program.parse(process.argv);

const provider = new ethers.JsonRpcProvider(program.rpc);

const addrValidatorSet = "0x0000000000000000000000000000000000001000";
const addrSlash = "0x0000000000000000000000000000000000001001";
const addrStakeHub = "0x0000000000000000000000000000000000002002";
const addrGovernor = "0x0000000000000000000000000000000000002004";

const validatorSetAbi = [
    "function validatorExtraSet(uint256 offset) external view returns (uint256, bool, bytes)",
    "function getLivingValidators() external view returns (address[], bytes[])",
    "function numOfCabinets() external view returns (uint256)",
    "function maxNumOfCandidates() external view returns (uint256)",
    "function maxNumOfWorkingCandidates() external view returns (uint256)",
    "function maxNumOfMaintaining() external view returns (uint256)", // default 3
    "function turnLength() external view returns (uint256)",
    "function systemRewardAntiMEVRatio() external view returns (uint256)",
    "function maintainSlashScale() external view returns (uint256)", // default 2, valid: 1->9
    "function burnRatio() external view returns (uint256)", // default: 10%
    "function systemRewardBaseRatio() external view returns (uint256)", // default: 1/16
];
const slashAbi = [
    "function getSlashIndicator(address validatorAddr) external view returns (uint256, uint256)",
    "function misdemeanorThreshold() external view returns (uint256)",
    "function felonyThreshold() external view returns (uint256)",
    "function felonySlashScope() external view returns (uint256)",
];

// https://github.com/bnb-chain/bsc-genesis-contract/blob/master/contracts/StakeHub.sol
const stakeHubAbi = [
    "function getValidatorElectionInfo(uint256 offset, uint256 limit) external view returns (address[], uint256[], bytes[], uint256)",
    "function getValidatorDescription(address validatorAddr) external view returns (tuple(string, string, string, string))",
    "function consensusToOperator(address consensusAddr) public view returns (address)",
    "function minSelfDelegationBNB() public view returns (uint256)", // default 2000, valid: 1000 -> 100,000
    "function maxElectedValidators() public view returns (uint256)", // valid: 1 -> 500
    "function unbondPeriod() public view returns (uint256)", // default 7days, valid: 3days ->30days
    "function downtimeSlashAmount() public view returns (uint256)", // default 10BNB, valid: 5 -> felonySlashAmount
    "function felonySlashAmount() public view returns (uint256)", // default 200BNB, valid: > max(100, downtimeSlashAmount)
    "function downtimeJailTime() public view returns (uint256)", // default 2days,
    "function felonyJailTime() public view returns (uint256)", // default 30days,
];

const governorAbi = [
    "function votingPeriod() public view returns (uint256)",
    "function lateQuorumVoteExtension() public view returns (uint64)", // it represents minPeriodAfterQuorum
];

const validatorSet = new ethers.Contract(addrValidatorSet, validatorSetAbi, provider);
const slashIndicator = new ethers.Contract(addrSlash, slashAbi, provider);
const stakeHub = new ethers.Contract(addrStakeHub, stakeHubAbi, provider);
const governor = new ethers.Contract(addrGovernor, governorAbi, provider);

const validatorMap = new Map([
    // BSC mainnet
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
    ["0x7E1FdF03Eb3aC35BF0256694D7fBe6B6d7b3E0c8", "LegendIII"],
    ["0x7b501c7944185130DD4aD73293e8Aa84eFfDcee7", "MathW"],
    ["0x58567F7A51a58708C8B40ec592A38bA64C0697De", "Legend"],
    ["0x460A252B4fEEFA821d3351731220627D7B7d1F3d", "Defibit"],
    ["0x8A239732871AdC8829EA2f47e94087C5FBad47b6", "The48Club"],
    ["0xD3b0d838cCCEAe7ebF1781D11D1bB741DB7Fe1A7", "BNBEve"],
    ["0xF8B99643fAfC79d9404DE68E48C4D49a3936f787", "Avengers"],
    ["0x4e5acf9684652BEa56F2f01b7101a225Ee33d23f", "HashKey"],
    ["0x9bb56C2B4DBE5a06d79911C9899B6f817696ACFc", "Feynman"],
    ["0xbdcc079BBb23C1D9a6F36AA31309676C258aBAC7", "Fuji"],
    ["0x38944092685a336CB6B9ea58836436709a2adC89", "Shannon"],
    ["0xfC1004C0f296Ec3Df4F6762E9EabfcF20EB304a2", "Aoraki"],
    ["0xa0884bb00E5F23fE2427f0E5eC9E51F812848563", "Coda"],
    ["0xe7776De78740f28a96412eE5cbbB8f90896b11A5", "Ankr"],
    ["0xA2D969E82524001Cb6a2357dBF5922B04aD2FCD8", "Pexmons"],
    ["0x5cf810AB8C718ac065b45f892A5BAdAB2B2946B9", "Zen"],
    ["0x4d15D9BCd0c2f33E7510c0de8b42697CA558234a", "LegendVII"],
    ["0x1579ca96EBd49A0B173f86C372436ab1AD393380", "LegendV"],
    ["0xd1F72d433f362922f6565FC77c25e095B29141c8", "LegendVI"],
    ["0xf9814D93b4d904AaA855cBD4266D6Eb0Ec1Aa478", "Legend8"],
    ["0x025a4e09Ea947b8d695f53ddFDD48ddB8F9B06b7", "Ciscox"],
    ["0xE9436F6F30b4B01b57F2780B2898f3820EbD7B98", "LegendIV"],
    ["0xC2d534F079444E6E7Ff9DabB3FD8a26c607932c8", "Axion"],
    ["0x9F7110Ba7EdFda83Fc71BeA6BA3c0591117b440D", "LegendIX"],
    ["0xB997Bf1E3b96919fBA592c1F61CE507E165Ec030", "Seoraksan"],
    ["0x286C1b674d48cFF67b4096b6c1dc22e769581E91", "Sigm8"],
    ["0x73A26778ef9509a6E94b55310eE7233795a9EB25", "Coinlix"],
    ["0x18c44f4FBEde9826C7f257d500A65a3D5A8edebc", "Nozti"],
    ["0xA100FCd08cE722Dc68Ddc3b54237070Cb186f118", "Tiollo"],
    ["0x0F28847cfdbf7508B13Ebb9cEb94B2f1B32E9503", "Raptas"],
    ["0xfD85346c8C991baC16b9c9157e6bdfDACE1cD7d7", "Glorin"],
    ["0x978F05CED39A4EaFa6E8FD045Fe2dd6Da836c7DF", "NovaX"],
    ["0xd849d1dF66bFF1c2739B4399425755C2E0fAbbAb", "Nexa"],
    ["0xA015d9e9206859c13201BB3D6B324d6634276534", "Star"],
    ["0x5ADde0151BfAB27f329e5112c1AeDeed7f0D3692", "Veri"],
    ["0xd6Ab358AD430F65EB4Aa5a1598FF2c34489dcfdE", "Saturn"],
    ["0x0dC5e1CAe4d364d0C79C9AE6BDdB5DA49b10A7d9", "ListaDAO"],
    ["0xE554F591cCFAc02A84Cf9a5165DDF6C1447Cc67D", "ListaDAO2"],
    ["0x059a8BFd798F29cE665816D12D56400Fa47DE028", "ListaDAO3"],
    ["0xEC3C2D51b8A6ca9Cf244F709EA3AdE0c7B21238F", "GlobalStk"],
    // Chapel
    ["0x08265dA01E1A65d62b903c7B34c08cB389bF3D99", "Ararat"],
    ["0x7f5f2cF1aec83bF0c74DF566a41aa7ed65EA84Ea", "Kita"],
    ["0x53387F3321FD69d1E030BB921230dFb188826AFF", "Fuji"],
    ["0x76D76ee8823dE52A1A431884c2ca930C5e72bff3", "Seoraksan"],
    ["0xd447b49CD040D20BC21e49ffEa6487F5638e4346", "Everest"],
    ["0x1a3d9D7A717D64e6088aC937d5aAcDD3E20ca963", "Elbrus"],
    ["0x40D3256EB0BaBE89f0ea54EDAa398513136612f5", "Bloxroute"],
    ["0xF9a1Db0d6f22Bd78ffAECCbc8F47c83Df9FBdbCf", "Test"],
    ["0xB4cd0dCF71381b452A92A359BbE7146e8825Ce46", "BSCLista"],
    ["0xD7b26968D8AD24f433d422b18A11a6580654Af13", "TNkgnL4"],
    ["0xFDA4C7E5C6005511236E24f3d5eBFd65Ffa10AED", "MyHave"],
    ["0x73f86c0628e04A69dcb61944F0df5DE115bA3FD8", "InfStones"],
    ["0xcAc5E4158fAb2eB95aA5D9c8DdfC9DF7208fDc58", "My5eVal"],
    ["0x96f6a2A267C726973e40cCCBB956f1716Bac7dc0", "ForDc0"],
    ["0x15a13e315CbfB9398A26D77a299963BF034c28F8", "Blxr"],
    ["0x1d622CAcd06c0eafd0a8a4C66754C96Db50cE14C", "Val9B"],
    ["0xd6BD505f4CFc19203875A92B897B07DE13d118ce", "Panipuri"],
    ["0x9532223eAa6Eb6939A00C0A39A054d93b5cCf4Af", "TrustT"],
    ["0xB19b6057245002442123371494372719d2Beb83D", "Vtwval"],
    ["0x5530Bac059E50821E7146D951e56FC7500bda007", "LedgrTrus"],
    ["0xEe22F03961b407bCBae66499a029Be4cA0AF4ab4", "AB4"],
    ["0x1AE5f5C3Cb452E042b0B7b9DC60596C9CD84BaF6", "Jake"],
    ["0xfA4d592F9B152f7a10B5DE9bE24C27a74BCE431A", "MyTWFMM"],
    ["0x26Ba9aB44feb5D8eE47aDeaa46a472f71E50fbce", "Lime"],
    ["0xC8824e38440893b62CaDC4d1BD05e33895B25d74", "Skynet3k"],
    ["0x9a2da2Ce5Eda5E0b4914720c3A798521956E1009", "Musala"],
    ["0x9270fF2EaA8ef253B57011A5b7505D948784E2be", "Vihren"],
    ["0x86eb31b90566a9f4F3AB85138c78A000EBA81685", "GucciOp3k"],
    ["0x28D70c3756d4939DCBdEB3f0fFF5B4B36E6e327F", "OmegaV"],
    ["0x6a5470a3B7959ab064d6815e349eD4aE2dE5210d", "Skynet10k"],
]);

const builderMap = new Map([
    // BSC mainnet
    //     blockrazor
    ["0x5532CdB3c0c4278f9848fc4560b495b70bA67455", "blockrazor dublin"],
    ["0xBA4233f6e478DB76698b0A5000972Af0196b7bE1", "blockrazor frankfurt"],
    ["0x539E24781f616F0d912B60813aB75B7b80b75C53", "blockrazor nyc"],
    ["0x49D91b1Ab0CC6A1591c2e5863E602d7159d36149", "blockrazor relay"],
    ["0x50061047B9c7150f0Dc105f79588D1B07D2be250", "blockrazor tokyo"],
    ["0x0557E8CB169F90F6eF421a54e29d7dd0629Ca597", "blockrazor virginia"],
    ["0x488e37fcB2024A5B2F4342c7dE636f0825dE6448", "blockrazor x"],
    //     puissant
    ["0x48a5Ed9abC1a8FBe86ceC4900483f43a7f2dBB48", "puissant ap"],
    ["0x487e5Dfe70119C1b320B8219B190a6fa95a5BB48", "puissant eu"],
    ["0x48FeE1BB3823D72fdF80671ebaD5646Ae397BB48", "puissant us"],
    ["0x48B4bBEbF0655557A461e91B8905b85864B8BB48", "puissant x"],
    ["0x4827b423D03a349b7519Dda537e9A28d31ecBB48", "puissant y"],
    ["0x48B2665E5E9a343409199D70F7495c8aB660BB48", "puissant:z"],
    //     blockroute
    ["0xD4376FdC9b49d90e6526dAa929f2766a33BFFD52", "blockroute dublin"],
    ["0x2873fc7aD9122933BECB384f5856f0E87918388d", "blockroute frankfurt"],
    ["0x432101856a330aafdeB049dD5fA03a756B3f1c66", "blockroute japan"],
    ["0x2B217a4158933AAdE6D6494e3791D454B4D13AE7", "blockroute nyc"],
    ["0x0da52E9673529b6E06F444FbBED2904A37f66415", "blockroute relay"],
    ["0xE1ec1AeCE7953ecB4539749B9AA2eEF63354860a", "blockroute singapore"],
    ["0x89434FC3a09e583F2cb4e47A8B8fe58De8BE6a15", "blockroute virginia"],
    ["0x10353562E662E333C0c2007400284e0e21cF74fF", "blockroute x"],
    //      jetbldr
    ["0x36CB523286D57680efBbfb417C63653115bCEBB5", "jetbldr ap"],
    ["0x3aD6121407f6EDb65C8B2a518515D45863C206A8", "jetbldr eu"],
    ["0x345324dC15F1CDcF9022E3B7F349e911fb823b4C", "jetbldr us"],
    //      blockbus
    ["0x3FC0c936c00908c07723ffbf2d536D6E0f62C3A4", "jetbldr dublin"],
    ["0x17e9F0D7E45A500f0148B29C6C98EfD19d95F138", "jetbldr tokyo"],
    ["0x1319Be8b8Ec4AA81f501924BdCF365fBcAa8d753", "jetbldr virginia"],
    //     txboost(blocksmith)
    ["0x6Dddf681C908705472D09B1D7036B2241B50e5c7", "txboost ap"],
    ["0x76736159984AE865a9b9Cc0Df61484A49dA68191", "txboost eu"],
    ["0x5054b21D8baea3d602dca8761B235ee10bc0231E", "txboost us"],
    //      darwin
    ["0xa6d6086222812eFD5292fF284b0F7ff2a2B86Af4", "darwin ap"],
    ["0x3265A3243ee84e667a73073504cA4CdeD1413D82", "darwin eu"],
    ["0xdf11CD23992Fd48Cf2d245aC144010673275f285", "darwin us"],
    //      inblock
    ["0x9a3234b450518fadA098388B88e00deCAd96ad38", "inblock ap"],
    ["0xb49f86586a840AB9920D2f340a85586E50FD30a2", "inblock eu"],
    ["0x0F6D8b72F3687de6f2824903a83B3ba13c0e88A0", "inblock us"],
    //      nodereal
    ["0x79102dB16781ddDfF63F301C9Be557Fd1Dd48fA0", "nodereal ap"],
    ["0xd0d56b330a0dea077208b96910ce452fd77e1b6f", "nodereal eu"],
    ["0x4f24ce4cd03a6503de97cf139af2c26347930b99", "nodereal us"],
    //      xzbuilder
    ["0x812720cb4639550D7BDb1d8F2be463F4a9663762", "xzbuilder"],

    // Chapel
    ["0x627fE6AFA2E84e461CB7AE7C2c46e8adf9a954a2", "txboost"],
    // ["0x79102dB16781ddDfF63F301C9Be557Fd1Dd48fA0", "nodereal ap"],
    // ["0x4827b423D03a349b7519Dda537e9A28d31ecBB48", "puissant y"],
    ["0x0eAbBdE133fbF3c5eB2BEE6F7c8210deEAA0f7db", "blockrazor"],
]);

// 1.cmd: "GetMaxTxCountInBlockRange", usage:
// node getchainstatus.js GetMaxTxCountInBlockRange --rpc https://bsc-testnet-dataseed.bnbchain.org \
//      --startNum 40000001  --endNum 40000005 \
//      --miner(optional): specified: find the max txCounter from the specified validator,
//                         not specified: find the max txCounter from all validators
async function getMaxTxCountInBlockRange() {
    let txCount = 0;
    let num = 0;
    console.log("Find the max txs count between", program.startNum, "and", program.endNum);
    for (let i = program.startNum; i < program.endNum; i++) {
        if (program.miner !== "") {
            let blockData = await provider.getBlock(Number(i));
            if (program.miner !== blockData.miner) {
                continue;
            }
        }
        let x = await provider.send("eth_getBlockTransactionCountByNumber", [ethers.toQuantity(i)]);
        let a = ethers.toNumber(x);
        if (a > txCount) {
            num = i;
            txCount = a;
        }
    }
    console.log("BlockNum = ", num, "TxCount =", txCount);
}

// 2.cmd: "GetBinaryVersion", usage:
// node getchainstatus.js GetBinaryVersion \
//      --rpc https://bsc-testnet-dataseed.bnbchain.org \
//       --num(optional): default 21, the number of blocks that will be checked
//       --turnLength(optional): default 4, the consecutive block length
async function getBinaryVersion() {
    const blockNum = await provider.getBlockNumber();
    let turnLength = program.turnLength;
    for (let i = 0; i < program.num; i++) {
        let blockData = await provider.getBlock(blockNum - i * turnLength);
        // 1.get Geth client version
        let major = ethers.toNumber(ethers.dataSlice(blockData.extraData, 2, 3));
        let minor = ethers.toNumber(ethers.dataSlice(blockData.extraData, 3, 4));
        let patch = ethers.toNumber(ethers.dataSlice(blockData.extraData, 4, 5));

        // 2.get minimum txGasPrice based on the last non-zero-gasprice transaction
        let lastGasPrice = 0;
        for (let txIndex = blockData.transactions.length - 1; txIndex >= 0; txIndex--) {
            let txHash = blockData.transactions[txIndex];
            let txData = await provider.getTransaction(txHash);
            if (txData.gasPrice == 0) {
                continue;
            }
            lastGasPrice = txData.gasPrice;
            break;
        }
        var moniker = await getValidatorMoniker(blockData.miner, blockNum);
        console.log(blockNum - i * turnLength, blockData.miner, "version =", major + "." + minor + "." + patch, " MinGasPrice = " + lastGasPrice, moniker);
    }
}

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

async function getTopAddr() {
    let countMap = new Map();
    let totalTxs = 0;
    console.log("Find the top target address, between", program.startNum, "and", program.endNum);
    for (let i = program.startNum; i <= program.endNum; i++) {
        let blockData = await provider.getBlock(Number(i), true);
        totalTxs += blockData.transactions.length;
        for (let txIndex = blockData.transactions.length - 1; txIndex >= 0; txIndex--) {
            let txData = await blockData.getTransaction(txIndex);
            if (txData.to == null) {
                console.log("Contract creation,txHash:", txData.hash);
                continue;
            }
            let toAddr = txData.to;
            if (countMap.has(toAddr)) {
                countMap.set(toAddr, countMap.get(toAddr) + 1);
            } else {
                countMap.set(toAddr, 1);
            }
        }
        console.log("progress:", program.endNum - i, "blocks left", "totalTxs", totalTxs);
    }
    let tops = getTopKElements(countMap, program.topNum);
    tops.forEach((value, key) => {
        // value: [ '0x40661F989826CC641Ce1601526Bb16a4221412c8', 71 ]
        console.log(key + ":", value[0], " ", value[1], " ", ((value[1] * 100) / totalTxs).toFixed(2) + "%");
    });
}

// 4.cmd: "GetSlashCount", usage:
// node getchainstatus.js GetSlashCount \
//      --rpc https://bsc-testnet-dataseed.bnbchain.org \
//      --blockNum(optional): the block num which is based for the slash state, default: latest block
async function getValidatorMoniker(consensusAddr, blockNum) {
    if (validatorMap.has(consensusAddr)) {
        return validatorMap.get(consensusAddr);
    }
    let opAddr = await stakeHub.consensusToOperator(consensusAddr, { blockTag: blockNum });
    let desc = await stakeHub.getValidatorDescription(opAddr, { blockTag: blockNum });
    let moniker = desc[0];
    console.log(consensusAddr, moniker);
    return moniker;
}

async function getSlashCount() {
    let blockNum = ethers.getNumber(program.blockNum);
    if (blockNum === 0) {
        blockNum = await provider.getBlockNumber();
    }
    let slashScale = await validatorSet.maintainSlashScale({ blockTag: blockNum });
    let maxElected = await stakeHub.maxElectedValidators({ blockTag: blockNum });
    const maintainThreshold = BigInt(50); // governable, hardcode to avoid one RPC call
    const felonyThreshold = BigInt(150); // governable, hardcode to avoid one RPC call

    let block = await provider.getBlock(blockNum);
    console.log("At block", blockNum, "time", block.date);
    const data = await validatorSet.getLivingValidators({ blockTag: blockNum });
    let totalSlash = 0;
    for (let i = 0; i < data[0].length; i++) {
        let addr = data[0][i];
        var moniker = await getValidatorMoniker(addr, blockNum);
        let info = await slashIndicator.getSlashIndicator(addr, { blockTag: blockNum });
        let slashHeight = ethers.toNumber(info[0]);
        let slashCount = ethers.toNumber(info[1]);
        totalSlash += slashCount;
        console.log("Slash:", slashCount, addr, moniker, slashHeight);
        if (slashCount >= maintainThreshold) {
            let validatorExtra = await validatorSet.validatorExtraSet(i, { blockTag: blockNum });
            let enterMaintenanceHeight = validatorExtra[0];
            let isMaintaining = validatorExtra[1];
            // let voteAddress = validatorExtra[2]
            if (isMaintaining) {
                let jailHeight = (felonyThreshold - BigInt(slashCount)) * slashScale * maxElected + BigInt(enterMaintenanceHeight);
                console.log("          in maintenance mode since", enterMaintenanceHeight, "will jail after", ethers.toNumber(jailHeight));
            } else {
                console.log("          exited maintenance mode");
            }
        }
    }
    console.log("Total slash count", totalSlash);
}

// 5.cmd: "getPerformanceData", usage:
// node getchainstatus.js getPerformanceData \
//      --rpc https://bsc-testnet-dataseed.bnbchain.org \
//      --startNum 40000001  --endNum 40000005
async function getPerformanceData() {
    let txCountTotal = 0;
    let gasUsedTotal = 0;
    let inturnBlocks = 0;
    let justifiedBlocks = 0;
    let turnLength = 4;
    let lastTimestamp = null; 
    let parliaEnabled = true;
    
    try {
        turnLength = await provider.send("parlia_getTurnLength", [ethers.toQuantity(program.startNum)]);
    } catch (error) {
        // the "parlia" module was not enabled for RPC call
        parliaEnabled = false;
        console.log("parlia RPC modudle is not enabled\n", error);
    }

    for (let i = program.startNum; i < program.endNum; i++) {
        let txCount = await provider.send("eth_getBlockTransactionCountByNumber", [ethers.toQuantity(i)]);
        txCountTotal += ethers.toNumber(txCount);

        let header = await provider.send("eth_getHeaderByNumber", [ethers.toQuantity(i)]);
        let gasUsed = eval(eval(header.gasUsed).toString(10));
        gasUsedTotal += gasUsed;
        let difficulty = eval(eval(header.difficulty).toString(10));
        if (difficulty == 2) {
            inturnBlocks += 1;
        }
        let timestamp = eval(eval(header.milliTimestamp).toString(10));
        let blockInterval = lastTimestamp !== null ? timestamp - lastTimestamp : null;
        console.log(
            "BlockNumber =",
            i,
            "mod =",
            i % turnLength,
            "miner =",
            header.miner,
            "difficulty =",
            difficulty,
            "txCount =",
            ethers.toNumber(txCount),
            "gasUsed",
            gasUsed,
            "timestamp",
            timestamp,
            "blockInterval",
            blockInterval !== null ? blockInterval : "N/A"
        );
        lastTimestamp = timestamp;

        if (parliaEnabled) {
            let justifiedNumber = await provider.send("parlia_getJustifiedNumber", [ethers.toQuantity(i)]);
            if (justifiedNumber + 1 == i) {
                justifiedBlocks += 1;
            } else {
                console.log("justified unexpected", "BlockNumber =", i, "justifiedNumber", justifiedNumber);
            }
        }
    }
    let blockCount = program.endNum - program.startNum;
    let txCountPerBlock = txCountTotal / blockCount;

    let startHeader = await provider.send("eth_getHeaderByNumber", [ethers.toQuantity(program.startNum)]);
    let startTime = eval(eval(startHeader.milliTimestamp).toString(10));
    let endHeader = await provider.send("eth_getHeaderByNumber", [ethers.toQuantity(program.endNum)]);
    let endTime = eval(eval(endHeader.milliTimestamp).toString(10));
    let timeCost = (endTime - startTime) / 1000;
    let avgBlockTime = timeCost / blockCount;
    let inturnBlocksRatio = inturnBlocks / blockCount;
    let justifiedBlocksRatio = justifiedBlocks / blockCount;
    let tps = txCountTotal / timeCost;
    let M = 1000000;
    let avgGasUsedPerBlock = gasUsedTotal / blockCount / M;
    let avgGasUsedPerSecond = gasUsedTotal / timeCost / M;

    console.log("Get the performance between [", program.startNum, ",", program.endNum, ")");
    console.log(
        "txCountPerBlock =",
        txCountPerBlock,
        "txCountTotal =",
        txCountTotal,
        "BlockCount =",
        blockCount,
        "avgBlockTime =",
        avgBlockTime,
        "inturnBlocksRatio =",
        inturnBlocksRatio,
        "justifiedBlocksRatio =",
        justifiedBlocksRatio
    );
    console.log("txCountPerSecond =", tps, "avgGasUsedPerBlock =", avgGasUsedPerBlock, "avgGasUsedPerSecond =", avgGasUsedPerSecond);
}

// 6.cmd: "GetBlobTxs", usage:
// node getchainstatus.js GetBlobTxs \
//      --rpc https://bsc-testnet-dataseed.bnbchain.org \
//      --startNum 40000001  --endNum 40000005
// depends on ethjs v6.11.0+ for 4844, https://github.com/ethers-io/ethers.js/releases/tag/v6.11.0
// BSC testnet enabled 4844 on block: 39539137
async function getBlobTxs() {
    var startBlock = parseInt(program.startNum);
    var endBlock = parseInt(program.endNum);
    if (isNaN(endBlock) || isNaN(startBlock) || startBlock == 0) {
        console.error("invalid input, --startNum", program.startNum, "--end", program.endNum);
        return;
    }
    // if --endNum is not specified, set it to the latest block number.
    if (endBlock == 0) {
        endBlock = await provider.getBlockNumber();
    }
    if (startBlock > endBlock) {
        console.error("invalid input, startBlock:", startBlock, " endBlock:", endBlock);
        return;
    }

    for (let i = startBlock; i <= endBlock; i++) {
        let blockData = await provider.getBlock(i);
        console.log("startBlock:", startBlock, "endBlock:", endBlock, "curBlock", i, "blobGasUsed", blockData.blobGasUsed);
        if (blockData.blobGasUsed == 0) {
            continue;
        }
        for (let txIndex = 0; txIndex <= blockData.transactions.length - 1; txIndex++) {
            let txHash = blockData.transactions[txIndex];
            let txData = await provider.getTransaction(txHash);
            if (txData.type == 3) {
                console.log("BlobTx in block:", i, " txIndex:", txIndex, " txHash:", txHash);
            }
        }
    }
}

// 7.cmd: "GetFaucetStatus", usage:
// node getchainstatus.js GetFaucetStatus \
//      --rpc https://bsc-testnet-dataseed.bnbchain.org \
//      --startNum 40000001  --endNum 40000005
async function getFaucetStatus() {
    var startBlock = parseInt(program.startNum);
    var endBlock = parseInt(program.endNum);
    if (isNaN(endBlock) || isNaN(startBlock) || startBlock == 0) {
        console.error("invalid input, --startNum", program.startNum, "--end", program.endNum);
        return;
    }
    // if --endNum is not specified, set it to the latest block number.
    if (endBlock == 0) {
        endBlock = await provider.getBlockNumber();
    }
    if (startBlock > endBlock) {
        console.error("invalid input, startBlock:", startBlock, " endBlock:", endBlock);
        return;
    }

    let startBalance = await provider.getBalance("0xaa25Aa7a19f9c426E07dee59b12f944f4d9f1DD3", startBlock);
    let endBalance = await provider.getBalance("0xaa25Aa7a19f9c426E07dee59b12f944f4d9f1DD3", endBlock);
    const faucetAmount = BigInt(0.3 * 10 ** 18); // Convert 0.3 ether to wei as a BigInt
    const numFaucetRequest = (startBalance - endBalance) / faucetAmount;

    // Convert BigInt to ether
    const startBalanceEth = Number(startBalance) / 10 ** 18;
    const endBalanceEth = Number(endBalance) / 10 ** 18;

    console.log(`Start Balance: ${startBalanceEth} ETH`);
    console.log(`End Balance: ${endBalanceEth} ETH`);

    console.log("successful faucet request: ", numFaucetRequest);
}

// 8.cmd: "GetKeyParameters", usage:
// node getchainstatus.js GetKeyParameters \
//      --rpc https://bsc-testnet-dataseed.bnbchain.org \
//      --blockNum(optional): the block num which is based for the slash state, default: latest block
async function getKeyParameters() {
    let blockNum = ethers.getNumber(program.blockNum);
    if (blockNum === 0) {
        blockNum = await provider.getBlockNumber();
    }
    let block = await provider.getBlock(blockNum);
    console.log("At block", blockNum, "time", block.date);

    // part 1: validatorset
    let numOfCabinets = await validatorSet.numOfCabinets({ blockTag: blockNum });
    if (numOfCabinets == 0) {
        numOfCabinets = 21;
    }
    // let maxNumOfCandidates = await validatorSet.maxNumOfCandidates({blockTag:blockNum})  // deprecated
    let turnLength = await validatorSet.turnLength({blockTag:blockNum})
    let maxNumOfWorkingCandidates = await validatorSet.maxNumOfWorkingCandidates({ blockTag: blockNum });
    let maintainSlashScale = await validatorSet.maintainSlashScale({ blockTag: blockNum });
    console.log("##==== ValidatorContract: 0x0000000000000000000000000000000000001000");
    console.log("\tturnLength", Number(turnLength));
    console.log("\tnumOfCabinets", Number(numOfCabinets));
    console.log("\tmaxNumOfWorkingCandidates", Number(maxNumOfWorkingCandidates));
    console.log("\tmaintainSlashScale", Number(maintainSlashScale));

    // part 2: slash
    let misdemeanorThreshold = await slashIndicator.misdemeanorThreshold({blockTag:blockNum})
    let felonyThreshold = await slashIndicator.felonyThreshold({blockTag:blockNum})
    let felonySlashScope = await slashIndicator.felonySlashScope({blockTag:blockNum})
    console.log("##==== SlashContract: 0x0000000000000000000000000000000000001001");
    console.log("\tmisdemeanorThreshold", Number(misdemeanorThreshold));
    console.log("\tfelonyThreshold", Number(felonyThreshold));
    console.log("\tfelonySlashScope", Number(felonySlashScope));


    // part 3: staking
    // let minSelfDelegationBNB = await stakeHub.minSelfDelegationBNB({blockTag:blockNum})/BigInt(10**18)
    let maxElectedValidators = await stakeHub.maxElectedValidators({ blockTag: blockNum });
    let validatorElectionInfo = await stakeHub.getValidatorElectionInfo(0, 0, { blockTag: blockNum });
    let consensusAddrs = validatorElectionInfo[0];
    let votingPowers = validatorElectionInfo[1];
    let voteAddrs = validatorElectionInfo[2];
    let totalLength = validatorElectionInfo[3];

    console.log("\n##==== StakeHubContract: 0x0000000000000000000000000000000000002002")
    console.log("\tmaxElectedValidators", Number(maxElectedValidators));
    console.log("\tRegistered", Number(totalLength));
    let validatorTable = [];
    for (let i = 0; i < totalLength; i++) {
        validatorTable.push({
            addr: consensusAddrs[i],
            votingPower: Number(votingPowers[i] / BigInt(10 ** 18)),
            voteAddr: voteAddrs[i],
            moniker: await getValidatorMoniker(consensusAddrs[i], blockNum),
        });
    }
    validatorTable.sort((a, b) => b.votingPower - a.votingPower);
    console.table(validatorTable);

    // part 4: governance
    let votingPeriod = await governor.votingPeriod({ blockTag: blockNum });
    let minPeriodAfterQuorum = await governor.lateQuorumVoteExtension({ blockTag: blockNum });
    console.log("\n##==== GovernorContract: 0x0000000000000000000000000000000000002004")
    console.log("\tvotingPeriod", Number(votingPeriod));
    console.log("\tminPeriodAfterQuorum", Number(minPeriodAfterQuorum));
}

// 9.cmd: "getEip7623", usage:
// node getEip7623.js GetEip7623 \
//      --rpc https://bsc-testnet-dataseed.bnbchain.org \
//      --startNum 40000001  --endNum 40000005
async function getEip7623() {
    var startBlock = parseInt(program.startNum);
    var endBlock = parseInt(program.endNum);
    if (isNaN(endBlock) || isNaN(startBlock) || startBlock === 0) {
        console.error("invalid input, --startNum", program.startNum, "--end", program.endNum);
        return;
    }
    // if --endNum is not specified, set it to the latest block number.
    if (endBlock === 0) {
        endBlock = await provider.getBlockNumber();
    }
    if (startBlock > endBlock) {
        console.error("invalid input, startBlock:", startBlock, " endBlock:", endBlock);
        return;
    }

    const startTime = Date.now();

    const TOTAL_COST_FLOOR_PER_TOKEN = 10;
    const intrinsicGas = 21000;
    for (let blockNumber = startBlock; blockNumber <= endBlock; blockNumber++) {
        const block = await provider.getBlock(blockNumber, true);

        for (let txHash of block.transactions) {
            let tx = block.getPrefetchedTransaction(txHash);
            const receipt = await provider.getTransactionReceipt(tx.hash);
            let tokens_in_calldata = -4; // means '0x'
            let calldata = tx.data;
            for (let i = 0; i < calldata.length; i += 2) {
                const byte = parseInt(calldata.substr(i, 2), 16);
                if (byte === 0) {
                    tokens_in_calldata++;
                } else {
                    tokens_in_calldata = tokens_in_calldata + 4;
                }
            }
            let want = TOTAL_COST_FLOOR_PER_TOKEN * tokens_in_calldata + intrinsicGas;
            if (want > receipt.gasUsed) {
                console.log("Cost more gas, blockNum:", tx.blockNumber, "txHash", tx.hash, " gasUsed", receipt.gasUsed.toString(), " New GasUsed", want);
            }
        }
    }
    const endTime = Date.now();
    const duration = (endTime - startTime) / 1000;
    console.log(`Script executed in: ${duration} seconds`);
}

// 10.cmd: "getMevStatus", usage:
// node getchainstatus.js GetMevStatus \
//      --rpc https://bsc-testnet-dataseed.bnbchain.org \
//      --startNum(optional): default to last 100 blocks, the start block number to analyze
//      --endNum(optional): default to latest block, the end block number to analyze
// 
// Description:
// Analyzes MEV (Maximal Extractable Value) blocks in a given range and displays:
// 1. Block-by-block information including:
//    - Block number
//    - Miner name (from validator set)
//    - Builder information (if MEV block) or "local" (if non-MEV block)
// 2. Statistics summary including:
//    - Block range analyzed
//    - Total number of blocks
//    - Distribution of blocks by builder type (local, blockrazor, puissant, blockroute, txboost)
//    - Percentage of each builder type
//
// Example:
// # Analyze last 100 blocks (default)
// node getchainstatus.js GetMevStatus --rpc https://bsc-testnet-dataseed.bnbchain.org
//
// # Analyze specific range
// node getchainstatus.js GetMevStatus --rpc https://bsc-testnet-dataseed.bnbchain.org --startNum 40000001 --endNum 40000005
//
// # Analyze from specific block to latest
// node getchainstatus.js GetMevStatus --rpc https://bsc-testnet-dataseed.bnbchain.org --startNum 40000001
async function getMevStatus() {
    let counts = {
        local: 0,
        blockrazor: 0,
        puissant: 0,
        blockroute: 0,
        jetbldr: 0,
        txboost: 0,
        blockbus: 0,
        darwin: 0,
        inblock: 0,
        nodereal: 0,
        xzbuilder: 0,
    };

    // Get the latest block number
    const latestBlock = await provider.getBlockNumber();
    
    // If startNum is not specified or is 0, use last 100 blocks
    let startBlock = parseInt(program.startNum, 10);
    if (isNaN(startBlock) || startBlock === 0) {
        startBlock = Math.max(1, latestBlock - 99); // Ensure we don't go below block 1
    }

    // If endNum is not specified or is 0, use the latest block number
    let endBlock = parseInt(program.endNum, 10);
    if (isNaN(endBlock) || endBlock === 0) {
        endBlock = latestBlock;
    }

    if (startBlock > endBlock) {
        console.error("Invalid input, startBlock:", startBlock, " endBlock:", endBlock);
        return;
    }

    const blockPromises = [];
    for (let i = startBlock; i <= endBlock; i++) {
        blockPromises.push(provider.getBlock(i));
    }

    const blocks = await Promise.all(blockPromises);

    // Calculate max lengths for alignment with default values
    let maxMinerLength = 10; // Default length
    let maxBuilderLength = 20; // Default length

    if (validatorMap.size > 0) {
        const minerLengths = Array.from(validatorMap.values()).map(m => m.length);
        maxMinerLength = Math.max(...minerLengths);
    }

    if (builderMap.size > 0) {
        const builderLengths = Array.from(builderMap.values()).map(b => b.length);
        maxBuilderLength = Math.max(...builderLengths);
    }

    for (const blockData of blocks) {
        const miner = validatorMap.get(blockData.miner) || "Unknown";
        const transactions = blockData.transactions.slice(-4); // Last 4 transactions
        const txPromises = transactions.map(txHash => provider.getTransaction(txHash));

        const txResults = await Promise.all(txPromises);
        let mevBlock = false;

        for (const txData of txResults) {
            if (builderMap.has(txData.to)) {
                const builder = builderMap.get(txData.to);
                const builderKey = Object.keys(counts).find(key => builder.includes(key));

                if (builderKey) {
                    counts[builderKey]++;
                }

                mevBlock = true;
                console.log(
                    `blockNum: ${blockData.number.toString().padStart(8)}      ` +
                    `miner: ${miner.padEnd(maxMinerLength)}        ` +
                    `builder: (${builder.padEnd(maxBuilderLength)}) ${txData.to}`
                );
                break;
            }
        }

        if (!mevBlock) {
            counts.local++;
            console.log(
                `blockNum: ${blockData.number.toString().padStart(8)}      ` +
                `miner: ${miner.padEnd(maxMinerLength)}        ` +
                `builder: local`
            );
        }
    }

    const total = endBlock - startBlock + 1;
    console.log("\nMEV Statistics:");
    console.log(`Range: [${startBlock}, ${endBlock}]`);
    console.log(`Total Blocks: ${total}`);
    console.log("\nBuilder Distribution:");
    Object.entries(counts).forEach(([key, value]) => {
        const ratio = (value *100 / total).toFixed(2);
        console.log(`${key.padEnd(10)}: ${value.toString().padStart(3)} blocks (${ratio}%)`);
    });
}

// 11.cmd: "getLargeTxs", usage:
// node getchainstatus.js GetLargeTxs \
//      --rpc https://bsc-testnet-dataseed.bnbchain.org \
//      --startNum 40000001  --num 100 \
//      --gasUsedThreshold 5000000
async function getLargeTxs() {
    const startTime = Date.now();
    const gasUsedThreshold = program.gasUsedThreshold;
    const startBlock = parseInt(program.startNum);
    const size = parseInt(program.num) || 100;
    
    let actualStartBlock = startBlock;
    if (isNaN(startBlock) || startBlock === 0) {
        actualStartBlock = await provider.getBlockNumber() - size;
    }
    const endBlock = actualStartBlock + size;
    
    console.log(`Finding transactions with gas usage >= ${gasUsedThreshold} between blocks ${actualStartBlock} and ${endBlock-1}`);
    
    // Cache for validator monikers to avoid redundant RPC calls
    const validatorCache = new Map();
    
    // Process blocks in batches to avoid memory issues with large ranges
    const BATCH_SIZE = 50;
    let largeTxCount = 0;
    
    for (let batchStart = actualStartBlock; batchStart < endBlock; batchStart += BATCH_SIZE) {
        const batchEnd = Math.min(batchStart + BATCH_SIZE, endBlock);
        
        // Create promises for all blocks in the current batch
        const blockPromises = [];
        for (let i = batchStart; i < batchEnd; i++) {
            blockPromises.push(provider.getBlock(i, true));
        }
        
        // Process all blocks in parallel
        const blocks = await Promise.all(blockPromises);
        
        // Process each block
        const results = await Promise.all(blocks.map(async (blockData) => {
            if (!blockData || blockData.transactions.length === 0) {
                return [];
            }
            
            const blockResults = [];
            const potentialTxs = [];
            
            // First-pass filter: check gas limit from the block data
            for (let txIndex = 0; txIndex < blockData.transactions.length; txIndex++) {
                const txData = await blockData.getTransaction(txIndex);
                if (txData && txData.gasLimit >= gasUsedThreshold) {
                    potentialTxs.push(txData);
                }
            }
            if (potentialTxs.length === 0) {
                return [];
            }
            
            // Fetch all transaction receipts in parallel
            const receiptPromises = potentialTxs.map(tx => provider.getTransactionReceipt(tx.hash));
            const receipts = await Promise.all(receiptPromises);
            
            // Filter transactions by actual gas used
            const largeTxs = potentialTxs.filter((tx, index) => 
                receipts[index] && receipts[index].gasUsed >= gasUsedThreshold
            );
            if (largeTxs.length === 0) {
                return [];
            }
            
            // Get validator moniker (use cache if available)
            let moniker;
            if (validatorCache.has(blockData.miner)) {
                moniker = validatorCache.get(blockData.miner);
            } else {
                moniker = await getValidatorMoniker(blockData.miner, blockData.number);
                validatorCache.set(blockData.miner, moniker);
            }

            // Add details for each large transaction
            for (let i = 0; i < largeTxs.length; i++) {
                const tx = largeTxs[i];
                const receipt = receipts[potentialTxs.findIndex(p => p.hash === tx.hash)];
                
                blockResults.push({
                    blockNumber: blockData.number,
                    difficulty: Number(blockData.difficulty),
                    txHash: tx.hash,
                    gasUsed: Number(receipt.gasUsed),
                    miner: moniker
                });
            }
            
            return blockResults;
        }));
        
        // Flatten results and log
        const flatResults = results.flat();
        largeTxCount += flatResults.length;
        
        flatResults.forEach(result => {
            console.log(
                "block:", result.blockNumber, 
                "difficulty:", result.difficulty, 
                "txHash:", result.txHash, 
                "gasUsed:", result.gasUsed,
                "miner:", result.miner
            );
        });
        
        // Show progress
        console.log(`Progress: ${Math.min(batchEnd, endBlock) - actualStartBlock}/${endBlock - actualStartBlock} blocks processed (${Math.round((batchEnd - actualStartBlock) * 100 / (endBlock - actualStartBlock))}%)`);
    }
    
    const endTime = Date.now();
    const duration = (endTime - startTime) / 1000;
    console.log(`Found ${largeTxCount} large transactions in ${duration.toFixed(2)} seconds`);
}

const main = async () => {
    if (process.argv.length <= 2) {
        console.error("invalid process.argv.length", process.argv.length);
        printUsage();
        return;
    }
    const cmd = process.argv[2];
    if (cmd == "-h" || cmd == "--help") {
        printUsage();
        return;
    }
    if (cmd === "GetMaxTxCountInBlockRange") {
        await getMaxTxCountInBlockRange();
    } else if (cmd === "GetBinaryVersion") {
        await getBinaryVersion();
    } else if (cmd === "GetTopAddr") {
        await getTopAddr();
    } else if (cmd === "GetSlashCount") {
        await getSlashCount();
    } else if (cmd === "GetPerformanceData") {
        await getPerformanceData();
    } else if (cmd === "GetBlobTxs") {
        await getBlobTxs();
    } else if (cmd === "GetFaucetStatus") {
        await getFaucetStatus();
    } else if (cmd === "GetKeyParameters") {
        await getKeyParameters();
    } else if (cmd === "GetEip7623") {
        await getEip7623();
    } else if (cmd === "GetMevStatus") {
        await getMevStatus();
    } else if (cmd === "GetLargeTxs") {
        await getLargeTxs();
    } else {
        console.log("unsupported cmd", cmd);
        printUsage();
    }
};

main()
    .then(() => process.exit(0))
    .catch(error => {
        console.error(error);
        process.exit(1);
    });
