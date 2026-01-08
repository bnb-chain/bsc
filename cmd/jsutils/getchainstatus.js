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
program.option("--stepLength <Num>", "step length", 115200);
program.option("--stepNum <Num>", "step num", 1);
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
    console.log("  --rpc        specify the url of RPC endpoint");
    console.log("               mainnet: https://bsc-mainnet.nodereal.io/v1/cc07638d01a64904a662599433827378");
    console.log("               testnet: https://bsc-testnet.nodereal.io/v1/cc07638d01a64904a662599433827378");
    console.log("  --startNum   the start block number");
    console.log("  --endNum     the end block number");
    console.log("  --miner      the miner address");
    console.log("  --num        the number of blocks to be checked");
    console.log("  --topNum     the topNum of blocks to be checked");
    console.log("  --blockNum   the block number to be checked");
    console.log("  --stepLength the size of block num for each step, default: 115200(1 day)");
    console.log("  --stepNum    the step num, default: 1");
    console.log("\nExample:");
    console.log("  node getchainstatus.js GetMaxTxCountInBlockRange --rpc https://bsc-testnet-dataseed.bnbchain.org --startNum 40000001  --endNum 40000005");
    console.log("  node getchainstatus.js GetBinaryVersion --rpc https://bsc-testnet-dataseed.bnbchain.org --num 21 --turnLength 8");
    console.log("  node getchainstatus.js GetTopAddr --rpc https://bsc-testnet-dataseed.bnbchain.org --startNum 40000001  --endNum 40000010 --topNum 10");
    console.log("  node getchainstatus.js GetSlashCount --rpc https://bsc-testnet-dataseed.bnbchain.org --blockNum 40000001 --stepNum 1 --stepLength 115200"); // default: latest block
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
const TimelockContract = "0x0000000000000000000000000000000000002006";

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
    "function getValidators(uint256, uint256) external view returns(address[], address[], uint256)",
    "function getNodeIDs(address[] validatorsToQuery) external view returns(address[], bytes32[][])",
    ];


const governorAbi = [
    "function votingPeriod() public view returns (uint256)",
    "function lateQuorumVoteExtension() public view returns (uint64)", // it represents minPeriodAfterQuorum
];

const timelockAbi = [
    "function getMinDelay() public view returns (uint256)",
];

const validatorSet = new ethers.Contract(addrValidatorSet, validatorSetAbi, provider);
const slashIndicator = new ethers.Contract(addrSlash, slashAbi, provider);
const stakeHub = new ethers.Contract(addrStakeHub, stakeHubAbi, provider);
const governor = new ethers.Contract(addrGovernor, governorAbi, provider);
const timelock = new ethers.Contract(TimelockContract, timelockAbi, provider);

const validatorMap = new Map([
    // consensusAddr -> [moniker, operatorAddr, voteAddr]
    // BSC mainnet
    ['0x58567F7A51a58708C8B40ec592A38bA64C0697De', [ 'Legend'  , '0x773760b0708a5Cc369c346993a0c225D8e4043B1', '0x8d78def84b10ab93dbfff6980d1054a2bc561bcf0abf3daf6096849bf03744fd4a49392e4813dc2251d68f6f95f27ba7']],
    ['0x37e9627A91DD13e453246856D58797Ad6583D762', ['LegendII' , '0x343dA7Ff0446247ca47AA41e2A25c5Bbb230ED0A', '0xabd04e3688a7c071dbc7eb3d0ace1c06baf163fdc8ffc742fec16f09fa468d30778a3c533b944899d33ae3225a3aee07']],
    ['0xB4647b856CB9C3856d559C885Bed8B43e0846a47', [ 'CertiK'  , '0xe0761D6679aE9691C98C3f07867740b08f43e510', '0xa663982486c84b2f66d9391efe6875d30be1d907e55d9c4a5a224de92a5d8ff180cc4ebca44253fa5a9730cc89d61994']],
    ['0x502aECFE253E6AA0e8D2A06E12438FFeD0Fe16a0', [ 'BscScan' , '0x0C5c547215c6516603c3de9525abEf86f66D3A54', '0xb15df58914a6b751909f0558a9f9af8efca7d46e480fc24478d977dafe7daf5161b38c72e9e1ea4865c288ef5b8054ab']],
    ['0x5009317FD4F6F8FeEa9dAe41E5F0a4737BB7A7D5', ['NodeReal' , '0x7d0F8A6D1C8fbF929Dcf4847A31E30d14923Fa31', '0xb3a0de43e5a979f8d7a9ad04f8f3f102bdbb17ef0bf6ac9a8fce3f110f409d99828be80295da56c2c7a7becd3647ce40']],
    ['0xD3b0d838cCCEAe7ebF1781D11D1bB741DB7Fe1A7', [ 'BNBEve'  , '0xA3beF3479254a2ec123Bf4CBa34499f94D96Ee5b', '0xb39bdc22b7275d8456ee325424d33829d364270b1ecebb389318c7f0cb1f1c334f05d1d5e26472f2cd07f5d8025ea266']],
    ['0xCa503a7eD99eca485da2E875aedf7758472c378C', ['InfStones', '0xd34403249B2d82AAdDB14e778422c966265e5Fb5', '0x91dce99bbdc44ee9500ed1e5c864bc88ba518585c7e6de5e94d26ee216dd8a5e06c5d2fc740123976c9588787b54998c']],
    ['0x460A252B4fEEFA821d3351731220627D7B7d1F3d', [ 'Defibit' , '0x4DC1Bf52da103452097df48505A6D01020fFB22b', '0xb3e34a6e7967c4da80dd3e5227acb02c92f33a026bbce5e52c19b7d8746b7e55d3e29b9083de0bf334fdf8ac91bc1485']],
    ['0x4e5acf9684652BEa56F2f01b7101a225Ee33d23f', [ 'HashKey' , '0x78504de17d6Dba03387C7CaE5322B9C86bF3027f', '0x8bcfeba8fcafdc6b6f9016d5a0dd08e4685a13bffd8c2087f66bf7ca2dace7fbbc40c40824e30a84d3fe62a2ddcd5217']],
    ['0xF8de5e61322302b2c6e0a525cC842F10332811bf', [ 'Namelix' , '0xCA9EbBE042f975700F13e5e0D7683918533170BE', '0x8e69853df9edb142b5d596f93bfa14253a733cb9d2d5a7ed1fc345e248a8cae7f23f438930123eebf61c98785d846a8b']],
    ['0x7E1FdF03Eb3aC35BF0256694D7fBe6B6d7b3E0c8', ['LegendIII', '0xF2B1d86DC7459887B1f7Ce8d840db1D87613Ce7f', '0xa066981de27634c2d17a68333f6d9b0c8cd7e08882c397c6ad92d95f8c279d6ef9ea04a1e2f4c4cdfe7ea6015f367ccb']],
    ['0x9f1b7FAE54BE07F4FEE34Eb1aaCb39A1F7B6FC92', ['TWStaking', '0x5c38FF8Ca2b16099C086bF36546e99b13D152C4c', '0xabfb714bc2daced46244ccab91917be3d8271a995636b24aa44ff97e3033f41262b2dad5ce2b31734e4dd3912c5838ee']],
    ['0x8A239732871AdC8829EA2f47e94087C5FBad47b6', ['The48Club', '0xaACc290a1A4c89F5D7bc29913122F5982916de48', '0xadc9ae11a5f0da15082a4ded8abaeb73338984c06e2c1af2eb24232e00511e95b24e89291d689f6bedb13d5a398af2ec']],
    ['0xF8B99643fAfC79d9404DE68E48C4D49a3936f787', ['Avengers' , '0xa31A940ecFB4Cb9fE7884eB3C9a959Db79CbdC70', '0x8d68efc0951aa89bf89fd829dd021fac27712f0f046e70d2d33a8a1fc05554a16d26669a9adb33bb41aaff43df7d4da0']],
    ['0xbdcc079BBb23C1D9a6F36AA31309676C258aBAC7', [  'Fuji'   , '0x1AE5f5C3Cb452E042b0B7b9DC60596C9CD84BaF6', '0xa2564fd6f7101c1fdb441018a24d25672826953caa03b6717e26e8af1c38507dd570bdedf1cb270c376630823f101577']],
    ['0x9bb56C2B4DBE5a06d79911C9899B6f817696ACFc', [ 'Feynman' , '0xdFeAcaffE5EAF47E442ef2ddAeAea2f21a6d3f91', '0x90ba8c56b1a3c032a3a0395d0f42f3a00b4f50ce9dc41f819a601e8f17b00e7a4eba8a9af4c4b3fdd31d555ccf70058d']],
    ['0x75B851a27D7101438F45fce31816501193239A83', [ 'Figment' , '0x477cB5d87144b2a6d93f72e32f5E01a459260D68', '0xad9a5f4ae5ec7dd886b09a47021461ec6f1971b3558f31e622311e94714398c80573fd531e0e8b4c4c3b456c4a5b9bf6']],
    ['0xCcB42A9b8d6C46468900527Bc741938E78AB4577', [ 'Turing'  , '0x0E3cf208F4141C41da86d52C5F2076b1aB310E8F', '0xb1e211be938b9f77888cbdc7bd3787148a6d1653eb0e17603df69d67db30fa4857e11edec461e01e3e02cea2c7d2c5dd']],
    ['0x1cFDBd2dFf70C6e2e30df5012726F87731F38164', ['Tranchess', '0x5CE21461E6472914F5E4D5B296C72125f26ed462', '0x8e9879d77f0f25c8f6348135cef7477c2455f516bec180921d4c669eae7258857327674ace724b598c0df3fd41068ef8']],
    ['0x38944092685a336CB6B9ea58836436709a2adC89', [ 'Shannon' , '0xB58ac55EB6B10e4f7918D77C92aA1cF5bB2DEd5e', '0xafc1c041d36ee43ed51b1cb17b9dff14068e594b79a3c401a0bcf9fae9fd86324822fc0bb768f0b7dae76927b23d3954']],
    ['0x7b501c7944185130DD4aD73293e8Aa84eFfDcee7', [  'MathW'  , '0xB12e8137eF499a1d81552DB11664a9E617fd350A', '0xb7adb10448b8be5fc875af7df065a5ee57f2ace2cca77b37bbe2e30fd16481afe8a64fe4dd3aff03d14fb180a05bf6a0']],
    ['0xfC1004C0f296Ec3Df4F6762E9EabfcF20EB304a2', [ 'Aoraki'  , '0x0E89f8F61690E3115c176db4dcE8Bb0333259987', '0x86ae5148cfd22c25b61e69627053f5a5b4b6aeddc37f295f396c8dc7a2522e5593283d4ebbc630cd1b1d85d96bf95878']],
    ['0xa0884bb00E5F23fE2427f0E5eC9E51F812848563', [  'Coda'   , '0x2ADEdB37fC5AcF00B0f039026EF69DabdBEcE5D5', '0xb5b04493802da2420be3ea64169bee293bd1ff8879d512671991905af75245d15d137d1a048ba1d73e1cf365f0d32a08']],
    ['0xe7776De78740f28a96412eE5cbbB8f90896b11A5', [  'Ankr'   , '0xeace91702B20bc6Ee62034eC7f5162D9a94bFbE4', '0xa46d2951abe5a0800b41ae2425903a5ce33494d4e65e2ca9b9c1381c2cb7d294ebb5c2bc12d45189dada20af04528119']],
    ['0x5cf810AB8C718ac065b45f892A5BAdAB2B2946B9', [   'Zen'   , '0x3bA645FB613eB9E8A44f84B230C037C9972615C3', '0xb9d313d561cc9d7a90342230d649d6b001f339e21e4fb996e643fddb38a9cbb58e48cad1510d1d72dfb2949c0e97b882']],
    ['0xE9436F6F30b4B01b57F2780B2898f3820EbD7B98', ['LegendIV' , '0x487Ad3A6E2FFAa9f393ae839005aF2f4c00d9E63', '0xa97776f9db52767b6c05efa5542b39df1245b6cd761da8bcc84fb62e3ea140a133c1ecd3af22c5d7d49ba220061847a7']],
    ['0xA2D969E82524001Cb6a2357dBF5922B04aD2FCD8', [ 'Pexmons' , '0xe8cf6D03E42B66ae80E30eCE99374e8e0C4a90e2', '0x847aa5a938de7930502a5b1f61a3e5a9c6c77354120ddcb964a7f15768e64ec1db0ea803cf9b4e9001181874bbbd0c75']],
    ['0x4d15D9BCd0c2f33E7510c0de8b42697CA558234a', ['LegendVII', '0x31738238B6A4FCb00bA4De9ee923986b6Df55ae6', '0x8a81ab85d1ea9d9842484aca571512a78ed805c2398ce055d90fa0327b1467d45a00ddc183798193b50fcb15352095e1']],
    ['0xd1F72d433f362922f6565FC77c25e095B29141c8', ['LegendVI' , '0x8683DCb7A775dF4587eEfAD7c08ee647D1da32C1', '0xad890a6a46c502f80d0fe8fb6cc1cb4cf82c183c480269dbec60de63f1577572135ddfe967a06099c2d8e0b652184312']],
    ['0x286C1b674d48cFF67b4096b6c1dc22e769581E91', [  'Sigm8'  , '0x5Ce119c093cFe978dcfd00e09E173FB97685069E', '0xa2783aa7911798eb2b7e5a3c61832f65731e14789cf1d624de2c83535d7719b466cdb2057beff302eab6c3cbfb42ee23']],
    ['0x9F7110Ba7EdFda83Fc71BeA6BA3c0591117b440D', ['LegendIX' , '0xF9D1637D1e45e454F5ed1F7729a9AB55EC121E2c', '0x84cd17e013e84b9d665687723a33d2dfd4f0f943e4baed3eecf4f04f43bc50c2eda54bc339e2732798378fd81a73b665']],
    ['0x1579ca96EBd49A0B173f86C372436ab1AD393380', [ 'LegendV' , '0xE5572297718e1943A92BfEde2E67A060439e8EFd', '0xa9b09bf9a8c7e6bd80af5af6ff0572ecd2e207698b3335fdc28a57d6f680726c2db7e19321cc794dc63ecc8638e2ff9f']],
    ['0xf9814D93b4d904AaA855cBD4266D6Eb0Ec1Aa478', [ 'Legend8' , '0x0813D0D092b97C157A8e68A65ccdF41b956883ae', '0x9169fa34c99e4a6c5728c129eaf21ea1eebf9af84924ef9544c22821b4bcddbd0f14217e63c38a5ed148431c35c79c24']],
    ['0x025a4e09Ea947b8d695f53ddFDD48ddB8F9B06b7', [ 'Ciscox'  , '0x7B67A5Bfc93E8ad3C4C4d3fdc6a4029dD110Fa99', '0x84f567cc5e83e60aca36676674ccaa533ac4e3d095bad4f8a141dea717ca5fe3d20925a99a4327bb861e8714f0efbb15']],
    ['0x5ADde0151BfAB27f329e5112c1AeDeed7f0D3692', [  'Veri'   , '0xB7Fce9e05c681Fb92e867D9893255DCE3e8790a2', '0xb6267282cb29f87af27f03e67f499239c6eaed428a93265e4ac7977f3c5d76792ed777809439dd24618c805859ddcead']],
    ['0x73A26778ef9509a6E94b55310eE7233795a9EB25', [ 'Coinlix' , '0xA0fB25F82592F3cE965804a2f21B65783d66cF18', '0xb6cb7097e6d525abc704b8965a50416b7ddb3c69bf8d1740be0cf26605146df96ed7baa7f342a54f592423f9a8c95eb4']],
    ['0xC2d534F079444E6E7Ff9DabB3FD8a26c607932c8', [  'Axion'  , '0xC1DA8b99674137CC4971bF974cdC5157c8B86AaF', '0x8633993fd05c4b6293e6f2b8429a628cfa00ac7551eff4940eb56f1f480094b1eff8eb73a9f958f164e944c27e00565c']],
    ['0xEC3C2D51b8A6ca9Cf244F709EA3AdE0c7B21238F', ['GlobalStk', '0x655B5c56C351793b5Bc4672F9fCDD0871ab56933', '0xa0136e6b8d2812a4772c8fa6cca50646f65c3d60926e78445b2c1509a9d9fac19189235feaf914c678a3092350d14b63']],
    ['0xd849d1dF66bFF1c2739B4399425755C2E0fAbbAb', [  'Nexa'   , '0xd6579B6F3c036038239c51C3D2AE623bE1F23beD', '0xa0bbbfbf2714045f3740e000f297f3e810118f9082cc306c235a23bc1752b8c812fe5ad273219b09675ae356cf2d2d27']],
    ['0x978F05CED39A4EaFa6E8FD045Fe2dd6Da836c7DF', [  'NovaX'  , '0xe9E14034Dc9452c0ADb1Be7f6CAB16809036C4f7', '0xa693d66f1267fdcf9f2d8f08bd398252af79b8868921552ae70bcd00174444df4839e67aeb6d5db8d6b864fa83429f1c']],
    ['0x0dC5e1CAe4d364d0C79C9AE6BDdB5DA49b10A7d9', ['ListaDAO' , '0x7766A5EE8294343bF6C8dcf3aA4B6D856606703A', '0xa19eee71b397e9d88d43477eefdaa457ca7857c20011988042f56bab6ae1f97144d5b9686f3da8bbb0c5ee0858f6af11']],
    ['0xE554F591cCFAc02A84Cf9a5165DDF6C1447Cc67D', ['ListaDAO2', '0x5F04549BC77d85B1DCCAbD8208Eb08F4bD4d1ae4', '0xb3e3f2eaf96ba382a0dafb3810da159aa0cff44f62bc34abf9bad2f7112a3fe82065d9b40e755f7ce84f8299b6078422']],
    ['0x059a8BFd798F29cE665816D12D56400Fa47DE028', ['ListaDAO3', '0x5b2Bc987af8Ca44479ACfa5E49c513cDEeB63F96', '0xb9a778843e8918d1db88b9caabc0bd4f73feb28d75983eec7eabde65ccc34cc9260807f2222e770205392434645ef7a0']],
    ['0xB997Bf1E3b96919fBA592c1F61CE507E165Ec030', ['Seoraksan', '0x11F3A9d13F6f11F29A7c96922C5471F356BD129F', '0x8a8408f3840dff0b71c4e980d82634520e26d86c9b4a83794301d26019cebf7495875468b86f6e00d6da080b0452f4e0']],
    ['0xA015d9e9206859c13201BB3D6B324d6634276534', [  'Star'   , '0xBb772d6e37C3dDB0f08e05c10d05419dB54C9Fd8', '0xa516c1b45b6dbf1333a8b5accf95c7c33c67348370f918126c7dde43f16c9b14782548464da1bd09084bf2efd915e6fa']],
    ['0xfD85346c8C991baC16b9c9157e6bdfDACE1cD7d7', [ 'Glorin'  , '0x9941BCe2601fC93478DF9f5F6Cc83F4FFC1D71d8', '0xa82a4f20ec3e0480c7504ae56a2a32e5e317d284eb53e201baf9bba77af3e50aebdf0028fdf7fb87443bd9a32eb43601']],
    ['0xA100FCd08cE722Dc68Ddc3b54237070Cb186f118', [ 'Tiollo'  , '0xdcC46Bda9E79A11Ff9080F69CB1C0BCcc4737A34', '0x985af7f79f5ce390513493ef43ba70c2bd6ea163cc49dd73c6cb93cc00ea072f679c611789ef67a6244aa1125fdf4bd6']],
    ['0x0F28847cfdbf7508B13Ebb9cEb94B2f1B32E9503', [ 'Raptas'  , '0xFA5E69f880c0287f5543CFe9167Db7B83bD8Dd79', '0x896d0a453c9c79b3146dbd5be98d1bf502ce89d6c588fb66cecfe5d436baa25ec82c9470c4b536bd6d077b98ea574101']],
    ['0x18c44f4FBEde9826C7f257d500A65a3D5A8edebc', [  'Nozti'  , '0x95E105468b3a9E158df258ee385CA873cB566bF2', '0xa76a951b947eda0b4585730049bf08338c0e679071127f0f2f7e7dce542a655d69b24e7af4586ed20efc2764044c0b3c']],
    ['0xEdB69D7AE8fE7c21a33e0491e76db241C8e09a5F', ['BlkRazor' , '0x5eBAf404d466a1cc2d02684B6A3bB1D43dCB7586', '0xb23e281776590409333b1b36019390f7fadce505f55bfb98969cd3df6660bfe873b73e73d28aeef04bac40e3f4520df1']],
    ['0xd6Ab358AD430F65EB4Aa5a1598FF2c34489dcfdE', [ 'Saturn'  , '0x54A9c15A143dBFf49539dfF4e095ec8D09464A4A', '0x835a7608cb0888fa649aa4120e81b1ab8c54c894e01b3b1d8c458563bec901ba6bb0c5f356dca5b962392872480f3b4c']],
    ['0xCc767841fbB5b79B91EdF7a19EC5bd2F3D334fD8', [ 'Kraken'  , '0x4279baBE4293c0826810b6C59e40F9DA9e5fd45b', '0xaa7a4c76d38b9fe7f872bbc53ac172faa56c7db2ad4b4aea3af41de2c2df7738e82827f501f206ea82ad050b4ffead8a']],
    //  Testnet: Chapel
    ['0x08265dA01E1A65d62b903c7B34c08cB389bF3D99', [ 'Ararat'   , '0x341e228f22D4ec16297DD05A9d6347C74c125F66', '0x96f763f030b1adcfb369c5a5df4a18e1529baffe7feaec66db3dbd1bc06810f7f6f88b7be6645418a7e2a2a3f40514c2']],
    ['0x7f5f2cF1aec83bF0c74DF566a41aa7ed65EA84Ea', [  'Kita'    , '0x2716756EAF7F1B4f4DbB282A80efdbf96e90A644', '0x99e3849ef31887c0f880a0feb92f356f58fbd023a82f5311fc87a5883a662e9ebbbefc90bf13aa533c2438a4113804bf']],
    ['0x53387F3321FD69d1E030BB921230dFb188826AFF', [  'Fuji'    , '0xAf581B49EA5B09d69D86A8eD801EF0cEdA33Ae34', '0xaa39ebf1c38b190851e4db0588a3e90142c5299041fb8a0db3bb9a1fa4bdf0dae84ca37ee12a6b8c26caab775f0e007b']],
    ['0x76D76ee8823dE52A1A431884c2ca930C5e72bff3', ['Seoraksan' , '0x696606f04f7597F444265657C8c13039Fd759b14', '0x803af79641cf964cc001671017f0b680f93b7dde085b24bbc67b2a562a216f903ac878c5477641328172a353f1e493cf']],
    ['0xd447b49CD040D20BC21e49ffEa6487F5638e4346', [ 'Everest'  , '0x48a5394e098BC84958BbDD0a1A5cf651a638e7be', '0xad9fc6d1ec30e28016d3892b51a7898bd354cfe78643453fd3868410da412de7f2883180d0a2840111ad2e043fa403eb']],
    ['0x1a3d9D7A717D64e6088aC937d5aAcDD3E20ca963', [ 'Elbrus'   , '0xD17B7AD43Ee1e638f3994598283ACB28E6fd2786', '0x979974cd8ff90cbf097023dc8c448245ceff671e965d57d82eaf9be91478cfa0f24d2993e0c5f43a6c5a4cd998500230']],
    ['0x90409F56966B5da954166eB005Cb1A8790430BA1', [ 'Legend'   , '0x3D08A6360D6f505A717edFcb3A63F87C14D2A1a1', '0x962a2342bac4831c6de73fcb77ad08669aaaa0a2ba6c6973a02b8928dbe573d17864e48c3521f238ace1c16e160bb7f5']],
    ['0x31d8627d2EA8D673dA6dD2E6F3AbC87D8F2D56A9', ['Infinity2' , '0x4882328c14bb1a9a5c4F5E2B21bE345A72A1f638', '0xa39afe91ecb1c140f1a9032811f383d966c6be3ecd9b00c1a07c395196c85a96f15b27b940d3838a75ed06122ab2b0cf']],
    ['0xD7b26968D8AD24f433d422b18A11a6580654Af13', [ 'TNkgnL4'  , '0xD7b26968D8AD24f433d422b18A11a6580654Af13', '0xaa800571b4a5d458a16b1543f04ed2d8a13fdf908c883d149bc96471e540c9f6327cc919e3161dfb638cb000b2041abc']],
    ['0xFDA4C7E5C6005511236E24f3d5eBFd65Ffa10AED', [ 'MyHave'   , '0x563D4ADD8461dA7B617e0da08Bb096A0971DF49e', '0xa78f778c68ccc09509e6cd1219b34d71e0185fd78ccb9ddad6601a295d8f091441d81818df12337123150847c1c9566f']],
    ['0x73f86c0628e04A69dcb61944F0df5DE115bA3FD8', ['InfStones' , '0x73f86c0628e04A69dcb61944F0df5DE115bA3FD8', '0xaf088694a4de09646c96b9fa0e695b8be83caef104a871d0f78f750b8ccd05c83d91313a6c406a410eacef3066a9aed9']],
    ['0xcAc5E4158fAb2eB95aA5D9c8DdfC9DF7208fDc58', [ 'My5eVal'  , '0xe3505b7a3a5D8318cf80932966A517e0B1044e5e', '0xab85e51c31969adb69bddc706d138b2367fbfc565c208806635511bb3ce5c8c655cf2b0805685ce4c24a72e5b5ec380a']],
    ['0xF9a1Db0d6f22Bd78ffAECCbc8F47c83Df9FBdbCf', [  'Test'    , '0x114aF0fd7CC0AB355100f178D0a8d4e0f85d9B5A', '0xaade0f78a6b92b38c9f6d45ce8fb01da2b800100201cf0936b6b4b14c98af22edbe27df8aa197fca733891b5b6ca95db']],
    ['0x40D3256EB0BaBE89f0ea54EDAa398513136612f5', ['Bloxroute' , '0x15B301e92aEE7063f863Ad57Eb4A7BeAe80F9164', '0xa334b49d766ebe3eb9f6bdc163bd2c19aa7e8cee1667851ae0c1651f01c4cf7cf2cfcf8475bff3e99cab25b05631472d']],
    ['0x96f6a2A267C726973e40cCCBB956f1716Bac7dc0', [ 'ForDc0'   , '0x96f6a2A267C726973e40cCCBB956f1716Bac7dc0', '0xa2016e93247a9d2bfab606b9efd1c01d2815834ec4031887cbec78e83d9d33a57719abbf9ecd2bd501575081ea0f6d31']],
    ['0x15a13e315CbfB9398A26D77a299963BF034c28F8', [  'Blxr'    , '0xdc4250d990d4a268d06F92289A4Fd136d3B4a0b5', '0xb0183ea044211f468630233d2533b73307979c78a9486b33bb4ee04ca31a65f3e86fba804db7fe293fa643e6b72bb382']],
    ['0x1d622CAcd06c0eafd0a8a4C66754C96Db50cE14C', [  'Val9B'   , '0x14C891ec8c10d3058f69fD64C027E1187A10fc9B', '0x8486f337476c2a2a692795aaba91a7679f4707ce26f75f20cb682b926d31304835e1e2e489ca559b5c5dcf3dfa552f2f']],
    ['0xd6BD505f4CFc19203875A92B897B07DE13d118ce', ['Panipuri'  , '0xd453466ffac729f0e90c315c989FdD8f03eD50cF', '0xb4d267ec15d2245d4d1d8973957f6142f134094afe3d199850abc67ba1862dc9569ec2e27f05a8ce377ce9f18752966f']],
    ['0x9532223eAa6Eb6939A00C0A39A054d93b5cCf4Af', [ 'TrustT'   , '0x9532223eAa6Eb6939A00C0A39A054d93b5cCf4Af', '0x8d8e5940887d597f9b2175f8d1f6b3eba85f608cfe30d1340b9ad28f4380518330e9a9c4b79a6c8678f4c5d5e3e49748']],
    ['0xB19b6057245002442123371494372719d2Beb83D', [ 'Vtwval'   , '0xB19b6057245002442123371494372719d2Beb83D', '0x81c4bfed52701540b6740143b2f6734ba38beb18b579f0b9251191d2adc449f1796d5e2495d8dc0d5ed5c08e3dd6dad7']],
    ['0x5530Bac059E50821E7146D951e56FC7500bda007', ['LedgrTrus' , '0x5530Bac059E50821E7146D951e56FC7500bda007', '0x90d9d8bb5611c0b54a3addef61383cba54b6c4ac482cefc845c36acfb01bc6fa3f13446f43691401283b21b4bc0d47d5']],
    ['0xEe22F03961b407bCBae66499a029Be4cA0AF4ab4', [   'AB4'    , '0xEe22F03961b407bCBae66499a029Be4cA0AF4ab4', '0xa1fed513e5d1ded9f5347131011e61a1baebf4e132515e40148c153e2d5e47045fd8a1499b6f155df44bbe36e4e59d64']],
    ['0x1AE5f5C3Cb452E042b0B7b9DC60596C9CD84BaF6', [  'Jake'    , '0xc11ffd5B1F52F3259a714047ACC12Abcc116e3d7', '0x912dd08ff76d051821d56b83d4134020156cbfb6c1444e08cdc124add139c322131ef1c4da4f769c65c0c3dc5af0d9ce']],
    ['0xfA4d592F9B152f7a10B5DE9bE24C27a74BCE431A', [ 'MyTWFMM'  , '0xfA4d592F9B152f7a10B5DE9bE24C27a74BCE431A', '0xb949243399d897eee3f36fb63525a31f047d37669eab56ad9e2f50b6dda245b19cc106fa50e567f9bd069b720e25f20e']],
    ['0xB4cd0dCF71381b452A92A359BbE7146e8825Ce46', ['BSCLista'  , '0xEEa92f3bc1FB55571F3e0f0016c64bCb22d6a6A7', '0x845f542ccab20ef62e9cbf97c82264c9798c0e136ca5c41d2d130925b9d97d2cd2d3f81e81c90c1dae6c99dd517a2c61']],
    ['0x26Ba9aB44feb5D8eE47aDeaa46a472f71E50fbce', [  'Lime'    , '0xD829932AF22De772A90B6a79e77922E8e66B397F', '0xb65b3969adac86b8bf8a621ea2304efe3c822449770bcff0ec38165f081b6ff4c93c2afcc825b620a6f963a8e33a5896']],
    ['0xC8824e38440893b62CaDC4d1BD05e33895B25d74', ['Skynet3k'  , '0xFcF1c5FE7aC2c82aE81Df2B27cEcd5800fDb9cCE', '0xa3cba8a3adab0cc6d77b81993a0d3e387b5be77fe2e642aa2868096c8fd72aae93611ffdfc628f27a6e705ad4877908e']],
    ['0x9a2da2Ce5Eda5E0b4914720c3A798521956E1009', [ 'Musala'   , '0xdcbf0222Fb6DB0d37C57Be57048fb96B8Dbdf5A0', '0x863cfcf372a5d3e807b1b430843b1df9548b792e7e0a782e5b383a3ade791bd879395b30939aa034df32fb248dce7ed4']],
    ['0x9270fF2EaA8ef253B57011A5b7505D948784E2be', [ 'Vihren'   , '0x4Dbd356Efa1c492f922a9BCc52a0371C4dEd26E7', '0x89012006b370a2c72f6366fbcfc3e2e27bf4e5c00b93ba8bcceb1bb0190ef903106c32273080f2d25f91edf4499fd6b3']],
    ['0x86eb31b90566a9f4F3AB85138c78A000EBA81685', ['GucciOp3k' , '0x32a80e35dd123262E509799fEfb9D3eDddf1Ec27', '0x85ce80127b894373302f8fae84821674ad7ebf20ffe4df4ee788b8fa6ccadbc5d895f46ca1d7a4d07be0bf0c3324cc26']],
    ['0x28D70c3756d4939DCBdEB3f0fFF5B4B36E6e327F', [ 'OmegaV'   , '0x1875ADAe6aaAED32A87DbcD2eEbf076EA86caA35', '0xb8db8ca018a14d843b560d8fabde98c3d47ad6ceea9d510d3357e2ff687acf19f9431aa180b17222a3a9328238297a1a']],
    ['0x6a5470a3B7959ab064d6815e349eD4aE2dE5210d', ['Skynet10k' , '0xDD1fD7C74BaCCA08e1b88a24199F19aB1b1b9cE4', '0x81f13afdbd6976d9784a05619405df430314e2707050b32f29ae683b9ef89d285d1a227df3e31ac147016c4c7533be70']],
    ['0xce6cCa0DE7b3EB3fd0CcE4bc38cceF473166e4f4', ['Infinity'  , '0xc8A6Bfe0834FB99340a0Df253d79B7CaE25053b8', '0xa40f553889e9de6b4fe8005a06d7335fa061ae51ef5ba2b0c4ea477fcaa8f6de1650e318cf59824462b1831a725488da']],
    ['0xa7deE0bCAEb78849Ec4aD4e2f48688D2e9f2315B', ['KrakV'     , '0x6563AA29C30d9f80968c2fb7DFFed092a03FBdeD', '0x848ffc9a3fac00d9fbaebcb63f2b7c0a4747d9ffecd4b484073ad03d91584cb51af29870c1c8421b757f4f6fae813288']],
    ['0x32415e630B9B3489639dEE7de21274Ab64016226', ['Kraken'     , '0x70Cd30d9216AF7A5654D245e9F5c649b811aB2eB', '0xa80ebd07bd9d717bd538413e8830f673e63dfad496c901de324be5d16b0496aee39352ecfb84fa58d8d8a67746f8ae6c']],
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
    ["0xa5559F1761e6dCa79Ac0c7A301CCDcC71D378fee", "nodereal ap"],
    ["0x6C98EB21139F6E12db5b78a4AeD4d8eBA147FB7b", "nodereal eu"],
    ["0x4E8cbf5912717B212db5b450ae7737455A5cc0aF", "nodereal us"],
    ["0x4827b423D03a349b7519Dda537e9A28d31ecBB48", "club48 ap"],
    ["0x48B2665E5E9a343409199D70F7495c8aB660BB48", "club48 eu"],
    ["0x48B4bBEbF0655557A461e91B8905b85864B8BB48", "club48 us"],
    ["0x0eAbBdE133fbF3c5eB2BEE6F7c8210deEAA0f7db", "blockrazor ap"],
    ["0x95c8436143c82Ea4d3529A3ed8DDa9998F6daC5F", "blockrazor eu"],
    ["0xb71Ba9e570ee20E983De1d5aE01baf5dCB4e4299", "blockrazor us"],
    ["0x7b3ee856c98b1bb3689ef7f90477df2927fcbdb6",  "trustnet"],
    ["0xA8caEc0D68a90Ac971EA1aDEFA1747447e1f9871",  "blockroute"],
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
//       --turnLength(optional): default 8, the consecutive block length
async function getBinaryVersion() {
    const blockNum = await provider.getBlockNumber();
    let turnLength = program.turnLength;
    for (let i = 0; i < program.num; i++) {
        let blockData = await provider.getBlock(blockNum - i * turnLength);

        let major = 0, minor = 0, patch = 0;
        let commitID = "";

        try {
            major = ethers.toNumber(ethers.dataSlice(blockData.extraData, 2, 3));
            minor = ethers.toNumber(ethers.dataSlice(blockData.extraData, 3, 4));
            patch = ethers.toNumber(ethers.dataSlice(blockData.extraData, 4, 5));
            
            // Check version: >= 1.6.4 uses new format with commitID
            const isNewFormat = major > 1 || (major === 1 && minor > 6) || (major === 1 && minor === 6 && patch >= 4);
            
            if (isNewFormat) {
                const extraVanity = 28;
                let vanityBytes = ethers.getBytes(ethers.dataSlice(blockData.extraData, 0, extraVanity));

                let rlpLength = vanityBytes.length;
                if (vanityBytes[0] >= 0xC0 && vanityBytes[0] <= 0xF7) {
                    rlpLength = (vanityBytes[0] - 0xC0) + 1; 
                }
                
                const rlpData = ethers.dataSlice(blockData.extraData, 0, rlpLength);
                const decoded = ethers.decodeRlp(rlpData);
                
                if (Array.isArray(decoded) && decoded.length >= 2) {
                     const secondElemHex = decoded[1];
                     let secondElemStr = "";
                     try {
                         secondElemStr = ethers.toUtf8String(secondElemHex);
                     } catch (e) {
                         secondElemStr = secondElemHex;
                     }
                     
                     if (secondElemStr.length > 0 && secondElemStr !== "geth") {
                         commitID = secondElemStr.startsWith("0x") ? secondElemStr.substring(2) : secondElemStr;
                     }
                }
            }
        } catch (e) {
            console.log("Parsing failed:", e.message);
        }

        // Format version string
        let versionStr = major + "." + minor + "." + patch;
        if (commitID && commitID.length > 0) {
            versionStr = versionStr + "-" + commitID;
        }

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
        console.log(blockNum - i * turnLength, blockData.miner, "version =", versionStr, " MinGasPrice = " + lastGasPrice, moniker);
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
//      --stepLength(optional): the step of the block num, default: 115200, 115200 blocks ~= 1 day
//      --stepNum(optional): the step num, default: 1, only show the current slash count
async function getValidatorMoniker(consensusAddr, blockNum) {
    const minerInfo = validatorMap.get(consensusAddr);
    if (minerInfo) {
        return minerInfo[0];
    }
    let opAddr = await stakeHub.consensusToOperator(consensusAddr, { blockTag: blockNum });
    let desc = await stakeHub.getValidatorDescription(opAddr, { blockTag: blockNum });
    let moniker = desc[0];
    console.log("getValidatorMoniker", consensusAddr, moniker);
    return moniker;
}

async function getOperatorAddress(consensusAddr, blockNum) {
    if (validatorMap.has(consensusAddr)) {
        return validatorMap.get(consensusAddr)[1];
    }
    let opAddr = await stakeHub.consensusToOperator(consensusAddr, { blockTag: blockNum });
    console.log("getOperatorAddress", consensusAddr, opAddr);
    return opAddr;
}

async function getSlashCountAtHeight(num) {
    let blockNum = ethers.getNumber(num);
    if (blockNum === 0) {
        blockNum = await provider.getBlockNumber();
    }
    let slashScale = await validatorSet.maintainSlashScale({ blockTag: blockNum });
    let maxElected = await stakeHub.maxElectedValidators({ blockTag: blockNum });
    const maintainThreshold = BigInt(200); // governable, hardcode to avoid one RPC call
    const felonyThreshold = BigInt(600); // governable, hardcode to avoid one RPC call

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

async function getSlashCount() {
    let blockNum = ethers.getNumber(program.blockNum);
    if (blockNum === 0) {
        blockNum = await provider.getBlockNumber();
    }
    let stepLength = ethers.getNumber(program.stepLength);
    if (stepLength === 0) {
        stepLength = 115200;
    }
    let stepNum = ethers.getNumber(program.stepNum);
    if (stepNum === 0) {
        stepNum = 1;
    }
    for (let i = 0; i < stepNum; i++) {
        await getSlashCountAtHeight(blockNum);
        blockNum = blockNum - stepLength;
    }
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
    let turnLength = 8;
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
            consensusAddr: consensusAddrs[i],
            votingPower: Number(votingPowers[i] / BigInt(10 ** 18)),
            moniker: await getValidatorMoniker(consensusAddrs[i], blockNum),
            opAddr: await getOperatorAddress(consensusAddrs[i], blockNum),
            voteAddr: voteAddrs[i],
        });
    }
    validatorTable.sort((a, b) => b.votingPower - a.votingPower);
    console.table(validatorTable);
    // get EVN node ids
    console.log("\n\nGet Registered EVN Node IDs");
    let validators = await stakeHub.getValidators(0, 1000, { blockTag: blockNum });
    let operatorAddrs = validators[0];
    let nodeIdss = await stakeHub.getNodeIDs(Array.from(operatorAddrs), { blockTag: blockNum });
    let consensusAddrs2 = nodeIdss[0];
    let nodeIdArr = nodeIdss[1];
    for (let i = 0; i < consensusAddrs2.length; i++) {
        let addr = consensusAddrs2[i];
        let nodeId = nodeIdArr[i];
        if (nodeId.length > 0) {
            let moniker = await getValidatorMoniker(addr, blockNum);
            console.log(addr, moniker, "nodeId:", nodeId);
        }
    }

    // part 4: governance
    let votingPeriod = await governor.votingPeriod({ blockTag: blockNum });
    let minPeriodAfterQuorum = await governor.lateQuorumVoteExtension({ blockTag: blockNum });
    console.log("\n##==== GovernorContract: 0x0000000000000000000000000000000000002004")
    console.log("\tvotingPeriod", Number(votingPeriod));
    console.log("\tminPeriodAfterQuorum", Number(minPeriodAfterQuorum));

    // part 5: timelock
    let minDelay = await timelock.getMinDelay({ blockTag: blockNum });
    console.log("\n##==== TimelockContract: 0x0000000000000000000000000000000000002006")
    console.log("\tminDelay", Number(minDelay));
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
        ...Object.fromEntries([...new Set(builderMap.values())].map(builder => [builder, 0]))
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
        const minerLengths = Array.from(validatorMap.values()).map(m => m[0].length);
        maxMinerLength = Math.max(...minerLengths);
    }

    if (builderMap.size > 0) {
        const builderLengths = Array.from(builderMap.values()).map(b => b.length);
        maxBuilderLength = Math.max(...builderLengths);
    }

    for (const blockData of blocks) {
        const minerInfo = validatorMap.get(blockData.miner);
        const miner = minerInfo ? minerInfo[0] : "Unknown";
        const transactions = blockData.transactions.slice(-4); // Last 4 transactions
        const txPromises = transactions.map(txHash => provider.getTransaction(txHash));

        const txResults = await Promise.all(txPromises);
        let mevBlock = false;

        for (const txData of txResults) {
            if (builderMap.has(txData.to)) {
                const builder = builderMap.get(txData.to);
                counts[builder]++;

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
    const localRatio = (counts.local * 100 / total).toFixed(2);
    console.log(`${"local".padEnd(maxBuilderLength)}: ${counts.local.toString().padStart(3)} blocks (${localRatio}%)`);

    Object.entries(counts)
        .filter(([key, value]) => key !== "local" && value > 0)
        .sort(([a], [b]) => a.localeCompare(b))
        .forEach(([key, value]) => {
            const ratio = (value * 100 / total).toFixed(2);
            console.log(`${key.padEnd(maxBuilderLength)}: ${value.toString().padStart(3)} blocks (${ratio}%)`);
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
