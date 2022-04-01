package util

import (
	"errors"
	"net"
	"net/rpc"
	"strconv"
)

func NewRPCClient(localIpPort string, remoteIpPort string) (*rpc.Client, error) {
	laddr, err := net.ResolveTCPAddr("tcp", localIpPort)
	if err != nil {
		return nil, errors.New("cannot resolve local address: " + localIpPort)
	}
	raddr, err := net.ResolveTCPAddr("tcp", remoteIpPort)
	if err != nil {
		return nil, errors.New("cannot resolve remote address: " + remoteIpPort)
	}
	conn, err := net.DialTCP("tcp", laddr, raddr)
	if err != nil {
		return nil, err
	}
	return rpc.NewClient(conn), nil
}

func NewRPCServerWithIpPort(handler interface{}, listenIpPort string) error {
	apiHandler := rpc.NewServer()
	err := apiHandler.Register(handler)
	if err != nil {
		return errors.New("error registering API")
	}
	lAddr, err := net.ResolveTCPAddr("tcp", listenIpPort)
	if err != nil {
		return errors.New("cannot resolve address " + listenIpPort)
	}
	listener, err := net.ListenTCP("tcp", lAddr)
	if err != nil {
		return errors.New("cannot listen for at " + listenIpPort)
	}
	go apiHandler.Accept(listener)
	return nil
}

func NewRPCServerWithIp(handler interface{}, listenIp string) (string, error) {
	apiHandler := rpc.NewServer()
	err := apiHandler.Register(handler)
	if err != nil {
		return "", errors.New("error registering API")
	}
	listenAddr := listenIp + ":0"
	lAddr, err := net.ResolveTCPAddr("tcp", listenAddr)
	if err != nil {
		return "", errors.New("cannot resolve address " + listenAddr)
	}
	listener, err := net.ListenTCP("tcp", lAddr)
	if err != nil {
		return "", errors.New("cannot listen at " + listenAddr)
	}
	go apiHandler.Accept(listener)
	return listenIp + ":" + strconv.Itoa(listener.Addr().(*net.TCPAddr).Port), nil
}
