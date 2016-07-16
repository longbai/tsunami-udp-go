package client

import (
	"fmt"
	"net"
	"os"
)

/*------------------------------------------------------------------------
 * int create_tcp_socket(ttp_session_t *session,
 *                       const char *server_name, u_int16_t server_port);
 *
 * Establishes a new TCP control session for the given session object.
 * The TCP session is connected to the given Tsunami server; we return
 * the file descriptor of the socket on success and nonzero on error.
 * This will be an IPv6 socket if ipv6_yn is true and an IPv4 socket
 * otherwise.
 *------------------------------------------------------------------------*/
func connect(host string, port uint16) (*net.TCPConn, error) {
	server := fmt.Sprintf("%s:%v", host, port)

	connection, err := net.Dial("tcp", server)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not connect to", server, port, err)
		return nil, err
	}
	conn, _ := connection.(*net.TCPConn)
	conn.SetNoDelay(true)
	return conn, nil
}

/*------------------------------------------------------------------------
 * int create_udp_socket(ttp_parameter_t *parameter);
 *
 * Establishes a new UDP socket for data transfer, returning the file
 * descriptor of the socket on success and -1 on error.  The parameter
 * structure is used for setting the size of the UDP receive buffer.
 * This will be an IPv6 socket if ipv6_yn is true and an IPv4 socket
 * otherwise. The next available port starting from parameter->client_port
 * will be taken, and the value of client_port is updated.
 *------------------------------------------------------------------------*/
func UdpListen(param *Parameter) (*net.UDPConn, error) {
	var e error
	for higherPortAttempt := 0; higherPortAttempt < 256; higherPortAttempt++ {
		str := fmt.Sprintf(":%d", param.clientPort+uint16(higherPortAttempt))
		addr, err := net.ResolveUDPAddr("udp", str)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error in getting address information")
			return nil, err
		}
		udp, err := net.ListenUDP("udp", addr)
		if err == nil {
			if higherPortAttempt > 0 {
				fmt.Fprintf(os.Stderr,
					"Warning: there are %d other Tsunami clients running\n",
					higherPortAttempt)
			}
			err = udp.SetReadBuffer(int(param.udpBuffer))
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error in resizing UDP receive buffer")
			}
			param.clientPort += uint16(higherPortAttempt)
			return udp, nil
		}
		e = err
	}
	fmt.Fprintln(os.Stderr, "Error in creating UDP socket")
	return nil, e
}
