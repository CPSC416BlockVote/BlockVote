import argparse
import json
from random import sample, shuffle
import subprocess
import time
import os
import psutil
import random

clients, client_pids = [], []

def boot(i, server=""):
    cmd = ['./bin/416client', "-id", f"{i + 1}"]
    if server != "":
        cmd.append(f"-{server}")
    pid = subprocess.Popen(cmd)
    print(f"client {i+1} started.")
    return pid

def boot_multi(num, max_id=0, server=""):
    client_ids = [i for i in range(max_id, num + max_id)]
    shuffle(client_ids)
    for i in client_ids:
        clients.append(boot(i, server))
        time.sleep(0.1)
        for proc in psutil.process_iter():
            if proc.name() == "416client" and proc.pid not in client_pids:
                client_pids.append(proc.pid)
                break
    return

def main():
    parser = argparse.ArgumentParser(description='P2 Booting Script')
    parser.add_argument('-n', type=int, default=4,
                        help='number of clients in the chain')
    parser.add_argument('-thetis', action='store_true',
                        help='run clients on thetis server')
    parser.add_argument('-anvil', action='store_true',
                        help='run clients on anvil server')
    parser.add_argument('-remote', action='store_true',
                        help='run clients on remote server')
    args = parser.parse_args()

    # system settings
    num_clients = args.n
    server = ""
    if args.thetis:
        server = "thetis"
    elif args.anvil:
        server = "anvil"
    elif args.remote:
        server = "remote"
    max_id = 0

    # go build
    print("Building executables...")
    if not os.path.exists('./bin'):
        print("Create output folder.")
        os.mkdir('./bin')
    subprocess.call(['go', 'build', '-o', 'bin/416client', "./cmd/client"])
    print("Done.\n")

    if not os.path.exists('./logs'):
        os.mkdir('./logs')

    # start clients

    print("Starting clients...")
    boot_multi(num_clients, max_id, server)
    max_id += num_clients
                
    print("Done.\n")

    print("Active clients: ", client_pids)

    while True:
        # upon receiving keyboard interrupt:
        action = input("Press ENTER to kill all clients and exit...\n")

        if action == "":
            try:
                for client in clients:
                    client.terminate()
            except:
                pass

            os.system("killall 416client")
            return


if __name__ == "__main__":
    main()