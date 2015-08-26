package server

import (
	"errors"
	"fmt"
	// "io"
	"crypto/rand"
	"encoding/binary"
	"net"
	"os"

	"tsunami"
)

/* state of a transfer */
type Transfer struct {
	parameter *Parameter /* the TTP protocol parameters                */
	filename  string     /* the path to the file                       */
	file      *os.File   /* the open file that we're transmitting      */
	// FILE               *transcript   /* the open transcript file for statistics    */
	udp_fd      *net.UDPConn /* the file descriptor of our UDP socket      */
	udp_address *net.UDPAddr /* the destination for our file data          */
	ipd_current float64      /* the inter-packet delay currently in usec   */
	block       uint32       /* the current block that we're up to         */
}

/* state of a Tsunami session as a whole */
type Session struct {
	parameter  *Parameter   /* the TTP protocol parameters                */
	transfer   Transfer     /* the current transfer in progress, if any   */
	client_fd  *net.TCPConn /* the connection to the remote client        */
	session_id uint32       /* the ID of the server session, autonumber   */
}

/*------------------------------------------------------------------------
 * int ttp_accept_retransmit(ttp_session_t *session,
 *                           retransmission_t *retransmission,
 *                           u_char *datagram);
 *
 * Handles the given retransmission request.  The actions taken depend
 * on the nature of the request:
 *
 *   REQUEST_RETRANSMIT -- Retransmit the given block.
 *   REQUEST_RESTART    -- Restart the transfer at the given block.
 *   REQUEST_ERROR_RATE -- Use the given error rate to adjust the IPD.
 *
 * For REQUEST_RETRANSMIT messsages, the given buffer must be large
 * enough to hold (block_size + 6) bytes.  For other messages, the
 * datagram parameter is ignored.
 *
 * Returns 0 on success and non-zero on failure.
 *------------------------------------------------------------------------*/
var iteration = 0

func ttp_accept_retransmit(session *Session, retransmission tsunami.Retransmission, datagram []byte) error {
	xfer := &session.transfer
	param := session.parameter
	// static int       iteration = 0;
	// static char      stats_line[80];
	// int              status;
	// u_int16_t        type;

	request_type := retransmission.RequestType

	/* if it's an error rate notification */
	if request_type == tsunami.REQUEST_ERROR_RATE {

		/* calculate a new IPD */
		if retransmission.ErrorRate > param.error_rate {
			factor1 := (1.0 * float64(param.slower_num) / float64(param.slower_den)) - 1.0
			factor2 := float64(1.0+retransmission.ErrorRate-param.error_rate) / float64(100000.0-param.error_rate)
			xfer.ipd_current *= 1.0 + (factor1 * factor2)
		} else {
			xfer.ipd_current *= float64(param.faster_num) / float64(param.faster_den)
		}

		/* make sure the IPD is still in range, for later calculations */
		var cmp float64 = 10000.0
		if xfer.ipd_current < cmp {
			cmp = xfer.ipd_current
		}
		xfer.ipd_current = cmp
		if cmp < float64(param.ipd_time) {
			xfer.ipd_current = float64(param.ipd_time)
		}

		/* build the stats string */
		s := fmt.Sprintln(retransmission.ErrorRate, float32(xfer.ipd_current), param.ipd_time, xfer.block,
			100.0*xfer.block/param.block_count, session.session_id)

		// print a status report
		if (iteration % 23) == 0 {
			fmt.Println(" erate     ipd  target   block   %%done srvNr")
		}
		iteration += 1
		fmt.Println(s)

		// /* print to the transcript if the user wants */
		// if (param.transcript_yn)
		//     xscript_data_log(session, stats_line);

		/* if it's a restart request */
	} else if request_type == tsunami.REQUEST_RESTART {

		/* do range-checking first */
		if (retransmission.Block == 0) || (retransmission.Block > param.block_count) {
			return errors.New(fmt.Sprint("Attempt to restart at illegal block ", retransmission.Block))
		} else {
			xfer.block = retransmission.Block
		}

		/* if it's a retransmit request */
	} else if request_type == tsunami.REQUEST_RETRANSMIT {

		/* build the retransmission */
		err := build_datagram(session, retransmission.Block, tsunami.TS_BLOCK_RETRANSMISSION, datagram)
		if err != nil {
			return err
		}

		/* try to send out the block */
		_, err = xfer.udp_fd.WriteToUDP(datagram[:6+param.block_size], xfer.udp_address)
		if err != nil {
			return errors.New(fmt.Sprint("Could not retransmit block ", retransmission.Block, err))
		}

		/* if it's another kind of request */
	} else {
		return errors.New(fmt.Sprint("Received unknown retransmission request of type ", retransmission.RequestType))
	}

	return nil
}

/*------------------------------------------------------------------------
 * int ttp_authenticate(ttp_session_t *session, const u_char *secret);
 *
 * Given an active Tsunami session, returns 0 if we are able to
 * negotiate authentication successfully and a non-zero value
 * otherwise.
 *
 * The negotiation process works like this:
 *
 *     (1) The server [this process] sends 512 bits of random data
 *         to the client.
 *
 *     (2) The client XORs 512 bits of the shared secret onto this
 *         random data and responds with the MD5 hash of the result.
 *
 *     (3) The server does the same thing and compares the result.
 *         If the authentication succeeds, the server transmits a
 *         result byte of 0.  Otherwise, it transmits a non-zero
 *         result byte.
 *------------------------------------------------------------------------*/
func ttp_authenticate(session *Session, secret string) error {
	random := make([]byte, 64) /* the buffer of random data               */
	// u_char server_digest[16];  /* the MD5 message digest (for us)         */
	// u_char client_digest[16];  /* the MD5 message digest (for the client) */
	// int    i;

	/* obtain the random data */
	_, err := rand.Read(random)
	if err != nil {
		return err
	}

	_, err = session.client_fd.Write(random)
	if err != nil {
		return errors.New(fmt.Sprint("Could not send authentication challenge to client", err))
	}

	client_digest := make([]byte, 16)
	_, err = session.client_fd.Read(client_digest)
	if err != nil {
		return errors.New(fmt.Sprint("Could not read authentication response from client", err))
	}

	server_digest := tsunami.PrepareProof(random, []byte(secret))
	for i := 0; i < len(server_digest); i++ {
		if client_digest[i] != server_digest[i] {
			session.client_fd.Write([]byte{1})
			return errors.New("Authentication failed")
		}
	}

	/* try to tell the client it worked */
	_, err = session.client_fd.Write([]byte{0})
	if err != nil {
		return errors.New(fmt.Sprint("Could not send authentication confirmation to client", err))
	}

	return nil
}

/*------------------------------------------------------------------------
 * int ttp_negotiate(ttp_session_t *session);
 *
 * Performs all of the negotiation with the client that is done prior
 * to authentication.  At the moment, this consists of verifying
 * identical protocol revisions between the client and server.  Returns
 * 0 on success and non-zero on failure.
 *
 * Values are transmitted in network byte order.
 *------------------------------------------------------------------------*/
func ttp_negotiate(session *Session) error {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, tsunami.PROTOCOL_REVISION)

	count, e := session.client_fd.Write(buf)
	if e != nil {
		return e
	}

	if count < 4 {
		return errors.New("Could not send protocol revision number")
	}

	count, e = session.client_fd.Read(buf)
	if e != nil {
		return e
	}
	if count < 4 {
		return errors.New("Could not read protocol revision number")
	}

	x := binary.BigEndian.Uint32(buf)
	if x != tsunami.PROTOCOL_REVISION {
		return errors.New("Protocol negotiation failed")
	}
	return nil
}

/*------------------------------------------------------------------------
 * int ttp_open_port(ttp_session_t *session);
 *
 * Creates a new UDP socket for transmitting the file data associated
 * with our pending transfer and receives the destination port number
 * from the client.  Returns 0 on success and non-zero on failure.
 *------------------------------------------------------------------------*/
func ttp_open_port(session *Session) error {
	// struct sockaddr    *address;
	// int                 status;
	// u_int16_t           port;
	// u_char              ipv6_yn = session->parameter->ipv6_yn;

	/* create the address structure */
	var address net.Addr
	if session.parameter.client == "" {
		address = session.client_fd.RemoteAddr()
	} else {
		addr, err := net.ResolveIPAddr("ip", session.parameter.client)
		if err != nil {
			return err
		}
		address = addr
		if addr.Network() == "ipv6" {
			session.parameter.ipv6 = true
		}
	}

	/* read in the port number from the client */
	buf := make([]byte, 2)
	_, err := session.client_fd.Read(buf)
	if err != nil {
		return err
	}
	// if x, err := address.(*) {

	// }

	// if (ipv6_yn)
	// ((struct sockaddr_in6 *) address)->sin6_port = port;
	// else
	// ((struct sockaddr_in *)  address)->sin_port  = port;

	// /* print out the port number */
	// if (session->parameter->verbose_yn)
	// printf("Sending to client port %d\n", ntohs(port));

	// /* open a new datagram socket */
	// session->transfer.udp_fd = create_udp_socket(session->parameter);
	// if (session->transfer.udp_fd < 0)
	// return warn("Could not create UDP socket");

	// session->transfer.udp_address = address;
	return nil
}
