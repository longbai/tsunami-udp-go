package server

import (
	// "encoding/binary"
	// "errors"
	"fmt"
	"net"
	"os"
)

/*------------------------------------------------------------------------
 * int Listen(Parameter *parameter);
 *
 * Establishes a new TCP server socket, returning the file descriptor
 * of the socket on success and a negative value on error.  This will
 * be an IPv6 socket if ipv6_yn is true and an IPv4 socket otherwise.
 *------------------------------------------------------------------------*/
func Listen(param *Parameter) (net.Listener, error) {
	p := fmt.Sprintf(":%d", param.tcp_port)
	ln, err := net.Listen("tcp", p)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not create server socket on port %d", param.tcp_port)
		fmt.Fprintln(os.Stderr, err)
		return nil, err
	}
	return ln, nil
}

func createUdpSocket(param *Parameter, remoteAddr *net.UDPAddr) (*net.UDPConn, error) {
	conn, err := net.DialUDP("udp", nil, remoteAddr)
	if err != nil {
		return nil, err
	}
	err = conn.SetWriteBuffer(int(param.udp_buffer))
	if err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}
