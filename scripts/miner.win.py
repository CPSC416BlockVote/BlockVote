import argparse
import json
from random import sample, shuffle
import subprocess
import time
import os
import psutil
import random

def boot(i):
    pid = subprocess.Popen(['bin\\416miner.exe', "-id", f"miner{i + 1}", "-addr", "127.0.0.1:" + str(27201 + i), ">", f"./logs/miner{i+1}.txt", "2>&1"], shell=True)
    print(f"miner {i+1} started.")
    return pid

def boot_multi(num, max_id=0):
    miners = []
    miner_pids = []
    miner_ids = [i for i in range(max_id, num + max_id)]
    shuffle(miner_ids)
    for i in miner_ids:
        miners.append(boot(i))

        for proc in psutil.process_iter():
            if proc.name() == "416miner.exe" and proc.pid not in miner_pids:
                miner_pids.append(proc.pid)
                break
    return miners, miner_pids

def main():
    parser = argparse.ArgumentParser(description='P2 Booting Script')
    parser.add_argument('--n', type=int, default=4,
                        help='number of miners in the chain')
    args = parser.parse_args()

    # system settings
    num_miners = args.num_miners
    max_id = 0

    # go build
    print("Building executables...")
    if not os.path.exists('./bin'):
        print("Create output folder.")
        os.mkdir('./bin')
    subprocess.call(['go', 'build', '-o', './bin/416miner.exe', "./cmd/miner"])
    print("Done.\n")

    if not os.path.exists('./logs'):
        os.mkdir('./logs')

    # start miners

    print("Starting miners...")
    miners, miner_pids = boot_multi(num_miners, max_id)
    max_id += num_miners
                
    print("Done.\n")

    print("Active miners: ", miner_pids)

    while True:
        # upon receiving keyboard interrupt:
        print("\nEnter \"k n\" to kill miners, or")
        print("Enter \"s n\" to start more miners, or")
        action = input("Press ENTER to interupt...\n")

        if action == "":
            try:
                for miner in miners:
                    miner.terminate()
            except:
                pass

            os.system(f"taskkill /IM \"416miner.exe\" /F")
            return
        elif action[0] == "k":
            killedPids = sample(miner_pids, min(len(miner_pids) - 1, int(action.split()[1])))
            cmd = "taskkill"
            for pid in killedPids:
                miner_pids.remove(pid)
                cmd += f" /pid {pid}"
            cmd += " /F"
            os.system(cmd)
            print("Killed miners:", killedPids)
            print("Active miners:", miner_pids)
        elif action[0] == "s":
            temp_miners, temp_miner_pids = boot_multi(int(action.split()[1]), max_id)
            miners += temp_miners
            miner_pids += temp_miner_pids
            max_id += int(action.split()[1])
            print("Active miners: ", miner_pids)


if __name__ == "__main__":
    main()