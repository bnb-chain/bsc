# Example commands: (You can either given path to the directory which has all the .log files or just one .log file name)
# python reorg_analyzer.py /path/to/logs_directory 
# or
# python reorg_analyzer.py bsc.log
# or 
# python reorg_analyzer.py bsc.log.2024-10-3*
import re
import os
import argparse
from collections import defaultdict
import glob

validator_addr2monikey = {
    "0x37e9627A91DD13e453246856D58797Ad6583D762": "LegendII",
    "0xB4647b856CB9C3856d559C885Bed8B43e0846a47": "CertiK",
    "0x75B851a27D7101438F45fce31816501193239A83": "Figment",
    "0x502aECFE253E6AA0e8D2A06E12438FFeD0Fe16a0": "BscScan",
    "0xCa503a7eD99eca485da2E875aedf7758472c378C": "InfStones",
    "0x5009317FD4F6F8FeEa9dAe41E5F0a4737BB7A7D5": "NodeReal",
    "0x1cFDBd2dFf70C6e2e30df5012726F87731F38164": "Tranchess",
    "0xF8de5e61322302b2c6e0a525cC842F10332811bf": "Namelix",
    "0xCcB42A9b8d6C46468900527Bc741938E78AB4577": "Turing",
    "0x9f1b7FAE54BE07F4FEE34Eb1aaCb39A1F7B6FC92": "TWStaking",
    "0x7E1FdF03Eb3aC35BF0256694D7fBe6B6d7b3E0c8": "LegendIII",
    "0x7b501c7944185130DD4aD73293e8Aa84eFfDcee7": "MathW",
    "0x58567F7A51a58708C8B40ec592A38bA64C0697De": "Legend",
    "0x460A252B4fEEFA821d3351731220627D7B7d1F3d": "Defibit",
    "0x8A239732871AdC8829EA2f47e94087C5FBad47b6": "The48Club",
    "0xD3b0d838cCCEAe7ebF1781D11D1bB741DB7Fe1A7": "BNBEve",
    "0xF8B99643fAfC79d9404DE68E48C4D49a3936f787": "Avengers",
    "0x4e5acf9684652BEa56F2f01b7101a225Ee33d23f": "HashKey",
    "0x9bb56C2B4DBE5a06d79911C9899B6f817696ACFc": "Feynman",
    "0xbdcc079BBb23C1D9a6F36AA31309676C258aBAC7": "Fuji",
    "0x38944092685a336CB6B9ea58836436709a2adC89": "Shannon",
    "0xfC1004C0f296Ec3Df4F6762E9EabfcF20EB304a2": "Aoraki",
    "0xa0884bb00E5F23fE2427f0E5eC9E51F812848563": "Coda",
    "0xe7776De78740f28a96412eE5cbbB8f90896b11A5": "Ankr",
    "0xA2D969E82524001Cb6a2357dBF5922B04aD2FCD8": "Pexmons",
    "0x5cf810AB8C718ac065b45f892A5BAdAB2B2946B9": "Zen",
    "0x4d15D9BCd0c2f33E7510c0de8b42697CA558234a": "LegendVII",
    "0x1579ca96EBd49A0B173f86C372436ab1AD393380": "LegendV",
    "0xd1F72d433f362922f6565FC77c25e095B29141c8": "LegendVI",
    "0xf9814D93b4d904AaA855cBD4266D6Eb0Ec1Aa478": "Legend8",
    "0x025a4e09Ea947b8d695f53ddFDD48ddB8F9B06b7": "Ciscox",
    "0xE9436F6F30b4B01b57F2780B2898f3820EbD7B98": "LegendIV",
    "0xC2d534F079444E6E7Ff9DabB3FD8a26c607932c8": "Axion",
    "0x9F7110Ba7EdFda83Fc71BeA6BA3c0591117b440D": "LegendIX",
    "0xB997Bf1E3b96919fBA592c1F61CE507E165Ec030": "Seoraksan",
    "0x286C1b674d48cFF67b4096b6c1dc22e769581E91": "Sigm8",
    "0x73A26778ef9509a6E94b55310eE7233795a9EB25": "Coinlix",
    "0x18c44f4FBEde9826C7f257d500A65a3D5A8edebc": "Nozti",
    "0xA100FCd08cE722Dc68Ddc3b54237070Cb186f118": "Tiollo",
    "0x0F28847cfdbf7508B13Ebb9cEb94B2f1B32E9503": "Raptas",
    "0xfD85346c8C991baC16b9c9157e6bdfDACE1cD7d7": "Glorin",
    "0x978F05CED39A4EaFa6E8FD045Fe2dd6Da836c7DF": "NovaX",
    "0xd849d1dF66bFF1c2739B4399425755C2E0fAbbAb": "Nexa",
    "0xA015d9e9206859c13201BB3D6B324d6634276534": "Star",
    "0x5ADde0151BfAB27f329e5112c1AeDeed7f0D3692": "Veri"
}

def get_monikey(validator_addr, addr2monikey_lower):
    if validator_addr.lower() in addr2monikey_lower:
        value = addr2monikey_lower[validator_addr.lower()]
        return value
    return "Unknown"

def parse_logs(file_paths):
    # Regular expressions to match log lines
    re_import = re.compile(
        r't=.* lvl=info msg="Imported new chain segment" number=(\d+) '
        r'hash=([0-9a-fx]+) miner=([0-9a-zA-Zx]+)'
    )
    re_reorg = re.compile(
        r't=.* lvl=info msg="Chain reorg detected" number=(\d+) hash=([0-9a-fx]+) '
        r'drop=(\d+) dropfrom=([0-9a-fx]+) add=(\d+) addfrom=([0-9a-fx]+)'
    )

    # Dictionaries to store block information
    block_info = {}
    reorgs = []

    for log_file_path in file_paths:
        try:
            with open(log_file_path, 'r', encoding='utf-8') as f:
                for line in f:
                    # Check for imported block lines
                    match_import = re_import.search(line)
                    if match_import:
                        block_number = int(match_import.group(1))
                        block_hash = match_import.group(2)
                        miner = match_import.group(3)
                        block_info[block_hash] = {
                            'number': block_number,
                            'miner': miner
                        }
                        continue

                    # Check for reorg lines
                    match_reorg = re_reorg.search(line)
                    if match_reorg:
                        reorg_number = int(match_reorg.group(1))
                        reorg_hash = match_reorg.group(2)
                        drop_count = int(match_reorg.group(3))
                        drop_from_hash = match_reorg.group(4)
                        add_count = int(match_reorg.group(5))
                        add_from_hash = match_reorg.group(6)
                        reorgs.append({
                            'number': reorg_number,
                            'hash': reorg_hash,
                            'drop_count': drop_count,
                            'drop_from_hash': drop_from_hash,
                            'add_count': add_count,
                            'add_from_hash': add_from_hash
                        })
        except Exception as e:
            print(f"Error reading file {log_file_path}: {e}")

    return block_info, reorgs

def analyze_reorgs(block_info, reorgs):
    results = []
    validator_reorgs = defaultdict(lambda: {'count': 0, 'blocks': []})

    for reorg in reorgs:
        # Get the dropped and added block hashes
        dropped_hash = reorg['drop_from_hash']
        added_hash = reorg['add_from_hash']

        # Get miner information
        dropped_miner = block_info.get(dropped_hash, {}).get('miner', 'Unknown')
        added_miner = block_info.get(added_hash, {}).get('miner', 'Unknown')

        # Construct the result
        result = {
            'reorg_at_block': reorg['number'],
            'dropped_block_hash': dropped_hash,
            'added_block_hash': added_hash,
            'dropped_miner': dropped_miner,
            'added_miner': added_miner,
            'responsible_validator': added_miner
        }
        results.append(result)

        # Update the validator reorgs data
        validator = added_miner
        validator_reorgs[validator]['count'] += 1
        validator_reorgs[validator]['blocks'].append(reorg['number'])

    return results, validator_reorgs

def get_log_files(paths):
    log_files = []
    for path in paths:
        # Expand patterns
        expanded_paths = glob.glob(path)
        if not expanded_paths:
            print(f"No files matched the pattern: {path}")
            continue
        for expanded_path in expanded_paths:
            if os.path.isfile(expanded_path):
                log_files.append(expanded_path)
            elif os.path.isdir(expanded_path):
                # Get all files in the directory
                files_in_dir = [
                    os.path.join(expanded_path, f)
                    for f in os.listdir(expanded_path)
                    if os.path.isfile(os.path.join(expanded_path, f))
                ]
                log_files.extend(files_in_dir)
            else:
                print(f"Invalid path: {expanded_path}")
    if not log_files:
        print("No log files to process.")
        exit(1)
    return log_files

def main():
    parser = argparse.ArgumentParser(description='Analyze BSC node logs for reorgs.')
    parser.add_argument('paths', nargs='+', help='Path(s) to log files, directories, or patterns.')
    args = parser.parse_args()

    log_files = get_log_files(args.paths)

    print("Processing the following files:")
    for f in log_files:
        print(f" - {f}")

    block_info, reorgs = parse_logs(log_files)
    results, validator_reorgs = analyze_reorgs(block_info, reorgs)

    # Print the detailed reorg results
    for res in results:
        print(f"Reorg detected at block number {res['reorg_at_block']}:")
        print(f"  Dropped block hash: {res['dropped_block_hash']}")
        print(f"  Dropped miner: {res['dropped_miner']}")
        print(f"  Added block hash: {res['added_block_hash']}")
        print(f"  Added miner: {res['added_miner']}")
        print(f"  Validator responsible for reorg: {res['responsible_validator']}")
        print('-' * 60)

    # Print the aggregated summary
    print("\nAggregated Validators Responsible for Reorgs:\n")
    print(f"{'Validator Address':<46} {'Monikey':<16} {'Number of Reorgs':<16} {'Block Numbers'}")
    print('-' * 100)
    adde2monikey_lowercase = {key.lower(): value for key, value in validator_addr2monikey.items()}
    for validator, data in sorted(validator_reorgs.items(), key=lambda x: x[1]['count'], reverse=True):
        block_numbers = ', '.join(map(str, data['blocks']))
        monikey = get_monikey(validator, adde2monikey_lowercase)
        print(f"{validator:<46} {monikey:<16} {data['count']:<16} {block_numbers}")

if __name__ == '__main__':
    main()

# Example Output: 
# Reorg detected at block number 43989479:
#   Dropped block hash: 0x8f97c466adc41449f98a51efd6c9b0ee480373a0d87d23fe0cbc78bcedb32f34
#   Dropped miner: 0xB4647b856CB9C3856d559C885Bed8B43e0846a48
#   Added block hash: 0x057f65b6852d269b61766387fecfbeed5b360fb3ffc8d80a73d674c3ad3237cc
#   Added miner: 0x58567F7A51a58708C8B40ec592A38bA64C0697Df
#   Validator responsible for reorg: 0x58567F7A51a58708C8B40ec592A38bA64C0697Df
# ------------------------------------------------------------
# ... (additional reorg details)
# ------------------------------------------------------------

# Aggregated Validators Responsible for Reorgs:

# Validator Address                              Monikey          Number of Reorgs  Block Numbers
# ----------------------------------------------------------------------------------------------------
# 0x4e5acf9684652BEa56F2f01b7101a225Ee33d23g     HashKey          13               43962513, 43966037, 43971672, ...
# 0x58567F7A51a58708C8B40ec592A38bA64C0697Df     Legend           9                43989479, 43996288, 43998896, ...
# 0x7E1FdF03Eb3aC35BF0256694D7fBe6B6d7b3E0c9     LegendIII        4                43990167, 43977391, 43912043, ...
# ... (additional validators)
