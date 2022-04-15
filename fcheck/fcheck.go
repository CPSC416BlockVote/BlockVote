/*

This package specifies the API to the failure checking library to be
used in assignment 2 of UBC CS 416 2021W2.

You are *not* allowed to change the API below. For example, you can
modify this file by adding an implementation to Stop, but you cannot
change its API.

*/

package fchecker

import (
	"bytes"
	"encoding/gob"
	"errors"
	"log"
	"net"
	"strconv"
	"sync"
)
import "time"

////////////////////////////////////////////////////// DATA
// Define the message types fchecker has to use to communicate to other
// fchecker instances. We use Go's type declarations for this:
// https://golang.org/ref/spec#Type_declarations

// Heartbeat message.
type HBeatMessage struct {
	EpochNonce uint64 // Identifies this fchecker instance/epoch.
	SeqNum     uint64 // Unique for each heartbeat in an epoch.
}

// An ack message; response to a heartbeat.
type AckMessage struct {
	HBEatEpochNonce uint64 // Copy of what was received in the heartbeat.
	HBEatSeqNum     uint64 // Copy of what was received in the heartbeat.
}

// Notification of a failure, signal back to the client using this
// library.
type FailureDetected struct {
	UDPIpPort string    // The RemoteIP:RemotePort of the failed node.
	Timestamp time.Time // The time when the failure was detected.
}

////////////////////////////////////////////////////// API

type StartStruct struct {
	LocalIP                          string
	EpochNonce                       uint64
	HBeatRemoteIPHBeatRemotePortList []string
	LostMsgThresh                    uint8
}

////////////////////////////////////////////////////// VARIABLE
var stop chan int // signal go routines to stop
var mu sync.Mutex
var nRoutines int // indicate the number of routines fcheck lib currently runs

// cache
var localHBAddr *net.UDPAddr
var epochNonce uint64
var lostMsgThresh uint8
var notify chan FailureDetected

// Starts the fcheck library.

func Start(arg StartStruct) (ackLocalPort string, notifyCh <-chan FailureDetected, err error) {
	mu.Lock()
	defer mu.Unlock()
	if nRoutines > 0 {
		return "", nil, errors.New("fcheck library has already been started")
	}
	// resolve local ack address
	localAckAddr, err := net.ResolveUDPAddr("udp", arg.LocalIP+":0")
	if err != nil { // check inappropriate ip:port values
		return "", nil, errors.New("invalid ip for local ack address: " + arg.LocalIP)
	}

	// open local ack udp port
	ackConn, err := net.ListenUDP("udp", localAckAddr)
	if err != nil {
		return "", nil, errors.New("unable to listen udp at " + arg.LocalIP)
	}
	ackLocalPort = strconv.Itoa(ackConn.LocalAddr().(*net.UDPAddr).Port)

	// create signal channel for stopping
	stop = make(chan int, len(arg.HBeatRemoteIPHBeatRemotePortList))

	if arg.HBeatRemoteIPHBeatRemotePortList == nil {
		// ONLY arg.AckLocalIP is set
		//
		// Start fcheck without monitoring any node, but responding to heartbeats.
		// TODO
		// start responding go routine
		go Respond(ackConn)
		return ackLocalPort, nil, nil
	}
	// Else: ALL fields in arg are set
	// Start the fcheck library by monitoring a single node and
	// also responding to heartbeats.

	epochNonce = arg.EpochNonce
	lostMsgThresh = arg.LostMsgThresh

	// TODO
	// resolve addresses
	localHBAddr, err = net.ResolveUDPAddr("udp", arg.LocalIP+":0")
	if err != nil {
		ackConn.Close()
		return "", nil, errors.New("invalid ip for local udp address: " + arg.LocalIP)
	}
	var hbConns []*net.UDPConn
	for _, remoteIPPort := range arg.HBeatRemoteIPHBeatRemotePortList {
		remoteHBAddr, err := net.ResolveUDPAddr("udp", remoteIPPort)
		if err != nil {
			log.Println("[WARN] fcheck is unable to resolve address", remoteIPPort)
			continue
		}
		// dial udp
		hbConn, err := net.DialUDP("udp", localHBAddr, remoteHBAddr)
		if err != nil {
			log.Println("[WARN] fcheck is unable to dial", remoteIPPort)
			continue
		}
		hbConns = append(hbConns, hbConn)
	}

	notify = make(chan FailureDetected, 100) // must have capacity of at least 1
	go Respond(ackConn)
	for idx, hbConn := range hbConns {
		go Monitor(hbConn, arg.HBeatRemoteIPHBeatRemotePortList[idx], notify)
	}

	return ackLocalPort, notify, nil
}

func NewRemote(remoteIpPort string) error {
	if nRoutines == 0 {
		return errors.New("fcheck is not started")
	}
	remoteHBAddr, err := net.ResolveUDPAddr("udp", remoteIpPort)
	if err != nil {
		return errors.New("fcheck is unable to resolve address " + remoteIpPort)
	}
	// dial udp
	hbConn, err := net.DialUDP("udp", localHBAddr, remoteHBAddr)
	if err != nil {
		return errors.New("fcheck is unable to dial " + remoteIpPort)
	}
	go Monitor(hbConn, remoteIpPort, notify)
	return nil
}

func Monitor(conn *net.UDPConn, remoteIpPort string, notifyCh chan<- FailureDetected) {
	mu.Lock()
	nRoutines++
	mu.Unlock()
	defer conn.Close()
	var lostCount uint8 = 0
	var hbSeqNum uint64 = 0 // identifier that is an arbitrary number which uniquely identifies the heartbeat in an epoch.
	var rtt int64 = 3000000 // 3 seconds as the initial RTT value
	sentTime := make(map[uint64]time.Time)
	for {
		// populate heartbeat message
		hbMsg := HBeatMessage{
			EpochNonce: epochNonce,
			SeqNum:     hbSeqNum,
		}
		// send heartbeat
		//for i := 0; i < rand.New(rand.NewSource(time.Now().UnixNano())).Intn(2); i++ {
		//	conn.Write(encodeHBeatMessage(&hbMsg))
		//	//for i := 0; i < rand.New(rand.NewSource(time.Now().UnixNano())).Intn(2); i++ {
		//	//	conn.Write(encodeHBeatMessage(&HBeatMessage{epochNonce, 0}))
		//	//}
		//}
		conn.Write(encodeHBeatMessage(&hbMsg))
		//log.Println("[fcheck] Sending heartbeat #", hbSeqNum)
		// record sent time
		sentTime[hbSeqNum] = time.Now() //.Nanosecond() / int(time.Microsecond)
		// update heartbeat sequence number
		hbSeqNum++
		conn.SetReadDeadline(time.Now().Add(time.Duration(rtt) * time.Microsecond))
		// read until timeout or receive an ack with correct epoch nonce
		var acked bool = false
		ackMsg := AckMessage{}
		for { // read everything until timeout (wait for RTT time)
			recBuf := make([]byte, 1024)
			length, err := conn.Read(recBuf)
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// timeout error
				break
			} else if err != nil {
				// other error
				log.Println("[fcheck] Read error", err)
				time.Sleep(time.Duration(rtt) * time.Microsecond)
				break
			}
			ackMsg = decodeAckMessage(recBuf, length)
			if ackMsg.HBEatEpochNonce == epochNonce { // must ignore all acks that it receives that do not reference this latest EpochNonce.
				//log.Println("[fcheck] Received ack #", ackMsg.HBEatSeqNum)
				// When an ack message is received (even after the RTT timeout), the count of lost msgs must be reset to 0.
				lostCount = 0
				acked = true
				// update RTT estimator
				if t, ok := sentTime[ackMsg.HBEatSeqNum]; ok {
					//rtt = (rtt + time.Now().Nanosecond()/int(time.Microsecond) - t) / 2
					rtt = (rtt + time.Since(t).Microseconds()) / 2
					delete(sentTime, ackMsg.HBEatSeqNum)
					if rtt < 500000 {
						rtt = 500000 // cap lower bound to 500000
					}
					//log.Println("[fcheck] New RTT:", rtt)
				}
			}
		}

		select {
		case <-stop: // received signal from Stop() function
			// After the call to Stop() has returned, no failure notifications must be generated.
			conn.Close()
			mu.Lock()
			nRoutines--
			mu.Unlock()
			return
		default:
			if !acked {
				// If an ack message is not received in the appropriate RTT timeout interval, then the count of lost msgs should be incremented by 1.
				lostCount++
				log.Println("[fcheck] Message lost count:", lostCount)
				if lostCount >= lostMsgThresh {
					log.Println("[fcheck] Server at " + remoteIpPort + " failed.")
					// If a node X was detected as failed, then
					// (1) exactly one failure notification must be generated
					failureDetected := FailureDetected{
						UDPIpPort: remoteIpPort,
						Timestamp: time.Now(),
					}
					notifyCh <- failureDetected
					// (2) the library must stop monitoring node X after generating the notification.
					conn.Close()
					mu.Lock()
					nRoutines--
					mu.Unlock()
					return
				}
			}
		}
	}
}

func Respond(conn *net.UDPConn) {
	mu.Lock()
	nRoutines++
	mu.Unlock()
	defer conn.Close()
	for {
		// read
		conn.SetReadDeadline(time.Now().Add(time.Duration(1) * time.Second))
		recBuf := make([]byte, 1024)
		length, remoteAddr, err := conn.ReadFromUDP(recBuf)

		// select channel
		select {
		case <-stop: // received signal from Stop() function
			// After the call to Stop() has returned, heartbeats to the library should not be acknowledged.
			conn.Close()
			mu.Lock()
			nRoutines--
			mu.Unlock()
			return
		default:
			// process read result
			if err == nil { // upon receiving a heartbeat
				// decode heart beat message
				hbMessage, _ := decodeHBeatMessage(recBuf, length)
				// construct ack message
				ackMessage := AckMessage{
					HBEatEpochNonce: hbMessage.EpochNonce,
					HBEatSeqNum:     hbMessage.SeqNum,
				}
				//log.Println("[fcheck] Received heartbeat message for Epoch #", hbMessage.EpochNonce,
				//	"Sequence #", hbMessage.SeqNum)
				// send ack message
				conn.WriteToUDP(encodeAckMessage(&ackMessage), remoteAddr)
			}
		}
	}
}

// Tells the library to stop monitoring/responding acks.
func Stop() {
	if nRoutines == 0 {
		return
	}
	for i := 0; i < nRoutines; i++ {
		stop <- 1
	}
	// wait for go routine to exit
	for nRoutines != 0 {
		time.Sleep(10 * time.Millisecond)
	}
	close(stop)
}

func encodeAckMessage(ackMsg *AckMessage) []byte {
	var buf bytes.Buffer
	err := gob.NewEncoder(&buf).Encode(ackMsg)
	if err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func encodeHBeatMessage(hbMsg *HBeatMessage) []byte {
	var buf bytes.Buffer
	err := gob.NewEncoder(&buf).Encode(hbMsg)
	if err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func decodeHBeatMessage(buf []byte, len int) (HBeatMessage, error) {
	var decoded HBeatMessage
	err := gob.NewDecoder(bytes.NewBuffer(buf[0:len])).Decode(&decoded)
	if err != nil {
		return HBeatMessage{}, err
	}
	return decoded, nil
}

func decodeAckMessage(buf []byte, len int) AckMessage {
	var decoded AckMessage
	err := gob.NewDecoder(bytes.NewBuffer(buf[0:len])).Decode(&decoded)
	if err != nil {
		log.Println("[fcheck] Error decoding ack message.")
	}
	return decoded
}
