package server

import (
	"fmt"
	"net"

	. "tsunami"
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
		return nil, Warn("Could not create server socket on port ",
			param.tcp_port, err)
	}
	return ln, nil
}

/*------------------------------------------------------------------------
 * int create_udp_socket(ttp_parameter_t *parameter);
 *
 * Establishes a new UDP socket for data transfer, returning the file
 * descriptor of the socket on success and a negative value on error.
 * This will be an IPv6 socket if ipv6_yn is true and an IPv4 socket
 * otherwise.
 *------------------------------------------------------------------------*/
func createUdpSocket(param *Parameter, remoteAddr *net.UDPAddr) (*net.UDPConn, error) {
	conn, err := net.DialUDP("udp", nil, remoteAddr)
	if err != nil {
		return nil, Warn("Dial udp failed", remoteAddr)
	}
	err = conn.SetWriteBuffer(int(param.udp_buffer))
	if err != nil {
		Warn("Set udp buffer failed. size ", param.udp_buffer, err)
	}
	return conn, nil
}
