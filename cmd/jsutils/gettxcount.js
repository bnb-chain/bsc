import { ethers } from "ethers";
import program from "commander";

program.option("--rpc <rpc>", "Rpc");
program.option("--startNum <startNum>", "start num")
program.option("--endNum <endNum>", "end num")
program.parse(process.argv);

const provider = new ethers.JsonRpcProvider(program.rpc)

const main = async () => {
    let txCount = 0;
    let num = 0;
    console.log("Find the max txs count between", program.startNum, "and", program.endNum);
    for (let i = program.startNum; i < program.endNum; i++) {
         let x = await provider.send("eth_getBlockTransactionCountByNumber", [
            ethers.toQuantity(i)]);
         let a = ethers.toNumber(x)
         if (a > txCount) {
             num = i;
             txCount = a;
         }
    }
    console.log("BlockNum = ", num, "TxCount =", txCount);
};

main().then(() => process.exit(0))
    .catch((error) => {
        console.error(error);
        process.exit(1);
    });