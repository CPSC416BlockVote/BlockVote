import argparse
import numpy as np
import os

def main():
    parser = argparse.ArgumentParser(description='P2 Checker Script')
    parser.add_argument('-n', type=int, default=1,
                        help='number of clients in the system')
    args = parser.parse_args()

    num_clients = args.n

    # load committed txns
    committed_txns = np.genfromtxt("./txns.txt", delimiter=",", dtype='str')
    if committed_txns.ndim == 1:
        committed_txns = committed_txns[np.newaxis, :]

    # load votes
    votes = np.genfromtxt("./votes.txt", delimiter=",", dtype='str')

    # load valid txns
    valid_txns = []
    for i in range(num_clients):
        txns = np.genfromtxt(f"./client{i + 1}valid.txt", delimiter=",", dtype='str')
        if txns.ndim == 1:
            txns = txns[np.newaxis, :]
        valid_txns.append(txns)
    valid_txns = np.concatenate(valid_txns)

    # load invalid txns
    invalid_txns = []
    for i in range(num_clients):
        txns = np.genfromtxt(f"./client{i + 1}invalid.txt", delimiter=",", dtype='str')
        if txns.ndim == 1:
            txns = txns[np.newaxis, :]
        invalid_txns.append(txns)
    invalid_txns = np.concatenate(invalid_txns)

    # load conflicting txns
    conflict_txns = []
    for i in range(num_clients):
        txns = np.genfromtxt(f"./client{i + 1}conflict.txt", delimiter=",", dtype='str')
        if txns.ndim == 1:
            txns = txns[np.newaxis, :]
        conflict_txns.append(txns)
    conflict_txns = np.concatenate(conflict_txns)

    print("[Check 1: all valid transactions are committed and appear extactly once]")
    committed_txids = committed_txns[:, 0]
    res = [np.sum(committed_txids == txn[0]) == 1 for txn in valid_txns]
    if sum(res) == len(valid_txns):
        print("Test Passed")
    else:
        print("Test Failed")
        for succ, txn in zip(res, valid_txns):
            if not succ:
                print(txn)

    print("[Check 2: all invalid transactions will not be committed]")
    res = [np.sum(committed_txids == txn[0]) == 0 for txn in invalid_txns]
    if sum(res) == len(invalid_txns):
        print("Test Passed")
    else:
        for succ, txn in zip(res, invalid_txns):
            if not succ:
                print(txn)
        print("Test Failed")

    print("[Check 3: each group of conflicting transactions can only have exactly one committed]")
    counts = [np.sum(committed_txids == txn[0]) for txn in conflict_txns]

    flag = True

    voter = ""
    committed_count = 0
    for count, txn in zip(counts, conflict_txns):
        if txn[1] != voter:
            if voter != "" and committed_count != 1:
                print(voter)
                flag = False
            voter = txn[1]
            committed_count = 0
        committed_count += count
    if voter != "" and committed_count != 1:  # check last voter
        print(voter)
        flag = False

    if flag:
        print("Test Passed")
    else:
        print("Test Failed")

    print("[Check 4: Vote counts are correct]")
    voteCounts = {}
    for txn in valid_txns:
        voteCounts[txn[2]] = voteCounts.get(txn[2], 0) + 1
    for txn in conflict_txns:
        if np.sum(committed_txids == txn[0]) > 0:
            voteCounts[txn[2]] = voteCounts.get(txn[2], 0) + 1

    flag = True
    for cand, count in votes:
        if voteCounts.get(cand, 0) != int(count):
            print(cand, voteCounts.get(cand, 0), count)
            flag = False

    if flag:
        print("Test Passed")
    else:
        print("Test Failed")

if __name__ == "__main__":
    main()