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

// func createUdpSocket(param *Parameter) (*net.UDPConn, error) {

// }

// int create_udp_socket(ttp_parameter_t *parameter)
// {
//     int socket_fd;
//     int status;
//     int yes = 1;

//     /* create the socket */
//     socket_fd = socket(parameter->ipv6_yn ? AF_INET6 : AF_INET, SOCK_DGRAM, 0);
//     if (socket_fd < 0)
//     return warn("Error in creating UDP socket");

//     /* make the socket reuseable */
//     status = setsockopt(socket_fd, SOL_SOCKET, SO_REUSEADDR, &yes, sizeof(yes));
//     if (status < 0) {
//     close(socket_fd);
//     return warn("Error in configuring UDP socket");
//     }

//     /* set the transmit buffer size */
//     status = setsockopt(socket_fd, SOL_SOCKET, SO_SNDBUF, &parameter->udp_buffer, sizeof(parameter->udp_buffer));
//     if (status < 0) {
//     warn("Error in resizing UDP transmit buffer");
//     }

//     /* return the file desscriptor */
//     return socket_fd;
// }
