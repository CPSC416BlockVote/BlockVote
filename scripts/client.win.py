import argparse
import json
from random import sample, shuffle
import subprocess
import time
import os
import psutil
import random

clients, client_pids = [], []

def boot(i):
    pid = subprocess.Popen(['bin\\416client.exe', "-id", f"client{i + 1}"], shell=True)
    print(f"client {i+1} started.")
    return pid

def boot_multi(num, max_id=0):
    client_ids = [i for i in range(max_id, num + max_id)]
    shuffle(client_ids)
    for i in client_ids:
        clients.append(boot(i))
        time.sleep(0.1)
        for proc in psutil.process_iter():
            if proc.name() == "416client.exe" and proc.pid not in client_pids:
                client_pids.append(proc.pid)
                break
    return

def main():
    parser = argparse.ArgumentParser(description='P2 Booting Script')
    parser.add_argument('-n', type=int, default=4,
                        help='number of clients in the chain')
    args = parser.parse_args()

    # system settings
    num_clients = args.n
    max_id = 0

    # go build
    print("Building executables...")
    if not os.path.exists('./bin'):
        print("Create output folder.")
        os.mkdir('./bin')
    subprocess.call(['go', 'build', '-o', './bin/416client.exe', "./cmd/client"])
    print("Done.\n")

    if not os.path.exists('./logs'):
        os.mkdir('./logs')

    # start clients

    print("Starting clients...")
    boot_multi(num_clients, max_id)
    max_id += num_clients
                
    print("Done.\n")

    print("Active clients: ", client_pids)

    while True:
        # upon receiving keyboard interrupt:
        action = input("Press ENTER to interupt...\n")

        if action == "":
            try:
                for client in clients:
                    client.terminate()
            except:
                pass

            os.system(f"taskkill /IM \"416client.exe\" /F")
            return


if __name__ == "__main__":
    main()