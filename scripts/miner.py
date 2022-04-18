import argparse
import json
from random import sample, shuffle
import subprocess
import time
import os
import psutil
import random

miners, miner_pids = [], []

def boot(i):
    pid = subprocess.Popen(['./bin/416miner', "-id", f"miner{i + 1}", "-addr", "127.0.0.1:" + str(27201 + i)])
    print(f"miner {i+1} started.")
    return pid

def boot_multi(num, max_id=0):
    miner_ids = [i for i in range(max_id, num + max_id)]
    shuffle(miner_ids)
    for i in miner_ids:
        miners.append(boot(i))
        time.sleep(0.1)
        for proc in psutil.process_iter():
            if proc.name() == "416miner" and proc.pid not in miner_pids:
                miner_pids.append(proc.pid)
                break
    return

def main():
    parser = argparse.ArgumentParser(description='P2 Booting Script')
    parser.add_argument('-n', type=int, default=4,
                        help='number of miners in the chain')
    args = parser.parse_args()

    # system settings
    num_miners = args.n
    max_id = 0

    # go build
    print("Building executables...")
    if not os.path.exists('./bin'):
        print("Create output folder.")
        os.mkdir('./bin')
    subprocess.call(['go', 'build', '-o', 'bin/416miner', "./cmd/miner"])
    print("Done.\n")

    if not os.path.exists('./logs'):
        os.mkdir('./logs')

    # start miners

    print("Starting miners...")
    boot_multi(num_miners, max_id)
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

            os.system("killall 416miner")
            return
        elif action[0] == "k":
            killedPids = sample(miner_pids, min(len(miner_pids), int(action.split()[1])))
            cmd = "kill -9"
            for pid in killedPids:
                miner_pids.remove(pid)
                cmd += f" {pid}"
            os.system(cmd)
            print("Killed miners:", killedPids)
            print("Active miners:", miner_pids)
        elif action[0] == "s":
            boot_multi(int(action.split()[1]), max_id)
            max_id += int(action.split()[1])
            print("Active miners: ", miner_pids)


if __name__ == "__main__":
    main()