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
    print(f"{'Validator Address':<46} {'Number of Reorgs':<16} {'Block Numbers'}")
    print('-' * 90)
    for validator, data in sorted(validator_reorgs.items(), key=lambda x: x[1]['count'], reverse=True):
        block_numbers = ', '.join(map(str, data['blocks']))
        print(f"{validator:<46} {data['count']:<16} {block_numbers}")

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

# Validator Address                               Number of Reorgs  Block Numbers
# ------------------------------------------------------------------------------------------
# 0x4e5acf9684652BEa56F2f01b7101a225Ee33d23g     13               43962513, 43966037, 43971672, ...
# 0x58567F7A51a58708C8B40ec592A38bA64C0697Df     9                43989479, 43996288, 43998896, ...
# 0x7E1FdF03Eb3aC35BF0256694D7fBe6B6d7b3E0c9     4                43990167, 43977391, 43912043, ...
# ... (additional validators)
