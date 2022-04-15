import argparse
import json
from random import sample, shuffle
import subprocess
import time
import os
import psutil

def main():
    parser = argparse.ArgumentParser(description='P2 Booting Script')
    parser.add_argument('--num_miners', type=int, default=4,
                        help='number of miners in the chain')
    args = parser.parse_args()

    # system settings
    num_miners = args.num_miners

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
    miners = []
    miner_pids = [0 for _ in range(num_miners)]
    miner_ids = [i for i in range(num_miners)]
    shuffle(miner_ids)
    for i in miner_ids:
        with open('./config/miner_config.json', 'r+') as f:
            data = json.load(f)
            data['MinerID'] = f"miner{i + 1}"
            data['MinerAddr'] = "127.0.0.1:" + str(27201 + i)
            data['TracingIdentity'] = f"miner{i + 1}"
            f.seek(0)
            json.dump(data, f, indent=2)
            f.truncate()

        miners.append(subprocess.Popen(['bin\\416miner.exe', ">", f"./logs/miner{i+1}.txt", "2>&1"], shell=True))
        print(f"miner {i+1} started.")
        time.sleep(6)  # let process run until it reads the config

        for proc in psutil.process_iter():
            if proc.name() == "416miner.exe" and proc.pid not in miner_pids:
                miner_pids[i] = proc.pid
                break
                
    print("Done.\n")

    print("Active miners: ", miner_pids)

    while True:
        # upon receiving keyboard interrupt:
        print("\nEnter the number of miners to be killed, or")
        cnt = input("Press ENTER to interupt...\n")

        if cnt == "":
            try:
                for miner in miners:
                    miner.terminate()
            except:
                pass

            os.system(f"taskkill /IM \"416miner.exe\" /F")
            return
        elif cnt == "h":
            cmd = f"taskkill /pid {miner_pids[0]} /F"
            os.system(cmd)
            print("Killed miners:", miner_pids[0])
            miner_pids = miner_pids[1:]
            print("Active miners:", miner_pids)
        elif cnt == "t":
            cmd = f"taskkill /pid {miner_pids[-1]} /F"
            os.system(cmd)
            print("Killed miners:", miner_pids[-1])
            miner_pids = miner_pids[:-1]
            print("Active miners:", miner_pids)
        else:
            killedPids = sample(miner_pids, min(len(miner_pids) - 1, int(cnt)))
            cmd = "taskkill"
            for pid in killedPids:
                miner_pids.remove(pid)
                cmd += f" /pid {pid}"
            cmd += " /F"
            os.system(cmd)
            print("Killed miners:", killedPids)
            print("Active miners:", miner_pids)


if __name__ == "__main__":
    main()