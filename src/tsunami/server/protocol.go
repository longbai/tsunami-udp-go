package server

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"time"

	"tsunami"
)

/* state of a transfer */
type Transfer struct {
	parameter   *Parameter   /* the protocol parameters                */
	filename    string       /* the path to the file                       */
	file        *os.File     /* the open file that we're transmitting      */
	transcript  *os.File     /* the open transcript file for statistics    */
	udp_fd      *net.UDPConn /* the file descriptor of our UDP socket      */
	udp_address *net.UDPAddr /* the destination for our file data          */
	ipd_current float64      /* the inter-packet delay currently in usec   */
	block       uint32       /* the current block that we're up to         */
}

/* state of a Tsunami session as a whole */
type Session struct {
	parameter  *Parameter   /* the protocol parameters                */
	transfer   *Transfer    /* the current transfer in progress, if any   */
	client_fd  *net.TCPConn /* the connection to the remote client        */
	session_id uint32       /* the ID of the server session, autonumber   */
	last_block uint32
	iteration  uint32
}

func NewSession(id uint32, conn *net.TCPConn, param *Parameter) *Session {
	session := &Session{}
	session.transfer = &Transfer{}
	session.session_id = id
	session.client_fd = conn
	p := *param
	session.parameter = &p
	return session
}

func (t *Transfer) close() {
	if t.file != nil {
		t.file.Close()
		t.file = nil
	}
	if t.transcript != nil {
		t.transcript.Close()
		t.transcript = nil
	}
	if t.udp_fd != nil {
		t.udp_fd.Close()
		t.udp_fd = nil
	}
}

/*------------------------------------------------------------------------
 * int AcceptRetransmit(ttp_session_t *session,
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

func (session *Session) AcceptRetransmit(retransmission tsunami.Retransmission, datagram []byte) error {
	xfer := session.transfer
	param := session.parameter

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

		s := fmt.Sprintf("%6u %3.2fus %5uus %7u %6.2f %3u\n",
			retransmission.ErrorRate, float32(xfer.ipd_current), param.ipd_time, xfer.block,
			100.0*xfer.block/param.block_count, session.session_id)

		if (session.iteration % 23) == 0 {
			fmt.Fprintln(os.Stderr, " erate     ipd  target   block   done srvNr")
		}
		session.iteration += 1
		fmt.Fprintln(os.Stderr, s)

		session.XsriptDataLog(s)

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
		err := session.buildDatagram(retransmission.Block, tsunami.TS_BLOCK_RETRANSMISSION, datagram)
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
 * int Authenticate(ttp_session_t *session, const u_char *secret);
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
func (session *Session) Authenticate() error {
	secret := session.parameter.secret
	random := make([]byte, 64) /* the buffer of random data               */

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
 * int Negotiate(ttp_session_t *session);
 *
 * Performs all of the negotiation with the client that is done prior
 * to authentication.  At the moment, this consists of verifying
 * identical protocol revisions between the client and server.  Returns
 * 0 on success and non-zero on failure.
 *
 * Values are transmitted in network byte order.
 *------------------------------------------------------------------------*/
func (session *Session) Negotiate() error {
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
 * int OpenPort(ttp_session_t *session);
 *
 * Creates a new UDP socket for transmitting the file data associated
 * with our pending transfer and receives the destination port number
 * from the client.  Returns 0 on success and non-zero on failure.
 *------------------------------------------------------------------------*/
func (session *Session) OpenPort() error {
	/* create the address structure */
	var udp_address net.UDPAddr
	if session.parameter.client == "" {
		address := session.client_fd.RemoteAddr()
		addr := address.(*net.TCPAddr)
		udp_address.IP = addr.IP
		udp_address.Zone = addr.Zone
	} else {
		addr, err := net.ResolveIPAddr("ip", session.parameter.client)
		if err != nil {
			return err
		}
		if addr.Network() == "ipv6" {
			session.parameter.ipv6 = true
		}
		udp_address.IP = addr.IP
		udp_address.Zone = addr.Zone
	}

	if udp_address.String() == "" {
		return nil
	}

	/* read in the port number from the client */
	buf := make([]byte, 2)
	_, err := session.client_fd.Read(buf)
	if err != nil {
		return err
	}

	x := binary.BigEndian.Uint16(buf)
	udp_address.Port = int(x)

	if session.parameter.verbose {
		fmt.Fprintln(os.Stderr, "Sending to client port", x)
	}

	fd, err := createUdpSocket(session.parameter, &udp_address)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not create UDP socket", err)
		return err
	}
	session.transfer.udp_fd = fd
	session.transfer.udp_address = &udp_address
	return nil
}

func (session *Session) listFiles() error {
	/* The client requested listing of files and their sizes (dir command)
	 * Send strings:   NNN \0   name1 \0 len1 \0     nameN \0 lenN \0
	 */
	p := session.parameter
	total_files := session.parameter.total_files
	buf := new(bytes.Buffer)
	s := strconv.FormatInt(int64(total_files), 10)
	buf.WriteString(s)
	buf.WriteByte(0)
	for i := 0; i < int(total_files); i++ {
		buf.WriteString(p.file_names[i])
		buf.WriteByte(0)
		size := strconv.FormatUint(p.file_sizes[i], 10)
		buf.WriteString(size)
		buf.WriteByte(0)
	}
	session.client_fd.Write(buf.Bytes())
	b := make([]byte, 1)
	_, err := session.client_fd.Read(b)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read client list ack failed")
		return err
	}
	fmt.Fprintln(os.Stderr, "file list sent")
	return nil
}

/* execute the provided program on server side to see what files
 * should be gotten
 */
func (session *Session) executeHook() (string, error) {
	param := session.parameter
	fmt.Fprintln(os.Stderr, "Using allhook program:", param.allhook)
	cmd := exec.Command(param.allhook)
	data, err := cmd.Output()
	if len(data) > 32768 {
		data = data[:32768]
	}
	var l int
	var count int64
	if err == nil {
		l = len(data)
		var last int
		for i, v := range data {
			if v == '\n' {
				fmt.Println(" ", string(data[last:i]))
				count++
			}
			if v < ' ' {
				data[i] = 0
			}
		}
	}

	b := make([]byte, 10)
	buf := bytes.NewBuffer(b)

	buf.WriteString(strconv.FormatInt(int64(l), 10))
	session.client_fd.Write(b)

	buf.Reset()
	tsunami.BZero(b)
	buf.WriteString(strconv.FormatInt(count, 10))
	session.client_fd.Write(b)

	buf.Reset()
	tsunami.BZero(b)
	message := b[:8]
	_, err = session.client_fd.Read(message)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read hook response failed", err)
		return "", err
	}

	fmt.Println("Client response:", string(message))

	tsunami.BZero(b)
	if count > 0 {
		session.client_fd.Write(data)
		_, err = session.client_fd.Read(message)
		if err != nil {
			fmt.Fprintln(os.Stderr, "read hook filelist response failed", err)
			return "", err
		}
		fmt.Println("Sent file list, client response:", string(message))
		s, err := tsunami.ReadLine(session.client_fd, tsunami.MAX_FILENAME_LENGTH)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Could not read filename from client", err)
			return "", err
		}
		return s, nil
	}
	return "", nil
}

func (session *Session) sendMultipleFileNames() (string, error) {
	/* A multiple file request - sent the file names first,
	 * and next the client requests a download of each in turn (get * command)
	 */
	param := session.parameter
	b := make([]byte, 10)
	buf := bytes.NewBuffer(b)

	buf.WriteString(strconv.FormatInt(int64(param.file_name_size), 10))
	session.client_fd.Write(b)
	buf.Reset()
	tsunami.BZero(b)

	buf.WriteString(strconv.FormatInt(int64(param.total_files), 10))
	session.client_fd.Write(b)
	buf.Reset()
	tsunami.BZero(b)

	fmt.Println("\nSent multi-GET filename count and array size to client")
	readBuff := make([]byte, 8)
	n, err := session.client_fd.Read(readBuff)
	if err != nil {
		return "", err
	}
	fmt.Println("Client response: ", string(readBuff[:n]))

	buf2 := new(bytes.Buffer)

	for i := 0; i < int(param.total_files); i++ {
		buf2.WriteString(param.file_names[i])
		buf2.WriteByte(0)

	}
	session.client_fd.Write(buf2.Bytes())

	tsunami.BZero(readBuff)
	n, err = session.client_fd.Read(readBuff)
	if err != nil {
		return "", err
	}

	fmt.Println("Sent file list, client response:", string(readBuff[:n]))

	fname, err := tsunami.ReadLine(session.client_fd, tsunami.MAX_FILENAME_LENGTH)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not read filename from client")
		return "", err
	}
	return fname, nil
}

/*------------------------------------------------------------------------
 * int OpenTransfer(ttp_session_t *session);
 *
 * Tries to create a new TTP file request object for the given session
 * by reading the name of a requested file from the client.  If we are
 * able to negotiate the transfer successfully, we return 0.  If we
 * can't negotiate the transfer because of I/O or file errors, we
 * return a negative vlaue.
 *
 * The client is sent a result byte of 0 if the request is accepted
 * (because the file can be read) and a non-zero result byte otherwise.
 *------------------------------------------------------------------------*/
func (session *Session) OpenTransfer() error {
	if session.transfer != nil {
		session.transfer.close()
	}
	session.transfer = &Transfer{}

	/* read in the requested filename */
	filename, err := tsunami.ReadLine(session.client_fd, tsunami.MAX_FILENAME_LENGTH)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not read filename from client")
		return err
	}

	if filename == tsunami.TS_DIRLIST_HACK_CMD {
		return session.listFiles()
	}
	param := session.parameter
	if filename == "*" {
		if param.allhook != "" {
			filename, err = session.executeHook()

		} else {
			filename, err = session.sendMultipleFileNames()
		}
		if err != nil {
			return err
		}
	}

	if param.verbose {
		fmt.Println("Request for file:", filename)
	}

	f, err := os.OpenFile(filename, os.O_RDONLY, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "File", filename, "does not exist or cannot be read", err)
		_, err2 := session.client_fd.Write([]byte{8})
		if err2 != nil {
			fmt.Fprintln(os.Stderr, "Could not signal request failure to client")
		}
		return err
	}

	session.transfer.file = f

	/* begin round trip time estimation */
	tStart := time.Now()

	/* try to signal success to the client */
	_, err = session.client_fd.Write([]byte{0})
	if err != nil {
		fmt.Sprintln(os.Stderr, "Could not signal request approval to client", err)
		return err
	}

	fourBytes := make([]byte, 4)
	/* read in the block size, target bitrate, and error rate */
	_, err = session.client_fd.Read(fourBytes)
	if err != nil {
		fmt.Sprintln(os.Stderr, "Could not read block size", err)
		return err
	}
	param.block_size = binary.BigEndian.Uint32(fourBytes)

	_, err = session.client_fd.Read(fourBytes)
	if err != nil {
		fmt.Sprintln(os.Stderr, "Could not read target bitrate", err)
		return err
	}
	param.target_rate = binary.BigEndian.Uint32(fourBytes)

	_, err = session.client_fd.Read(fourBytes)
	if err != nil {
		fmt.Sprintln(os.Stderr, "Could not read error rate", err)
		return err
	}
	param.error_rate = binary.BigEndian.Uint32(fourBytes)

	/* end round trip time estimation */
	tEnd := time.Now()

	/* read in the slowdown and speedup factors */
	twoBytes := fourBytes[:2]
	_, err = session.client_fd.Read(twoBytes)
	if err != nil {
		fmt.Sprintln(os.Stderr, "Could not read slowdown numerator", err)
		return err
	}
	param.slower_num = binary.BigEndian.Uint16(twoBytes)

	_, err = session.client_fd.Read(twoBytes)
	if err != nil {
		fmt.Sprintln(os.Stderr, "Could not read slowdown denominator", err)
		return err
	}
	param.slower_den = binary.BigEndian.Uint16(twoBytes)

	_, err = session.client_fd.Read(twoBytes)
	if err != nil {
		fmt.Sprintln(os.Stderr, "Could not read speedup numerator", err)
		return err
	}
	param.faster_num = binary.BigEndian.Uint16(twoBytes)

	_, err = session.client_fd.Read(twoBytes)
	if err != nil {
		fmt.Sprintln(os.Stderr, "Could not read speedup denominator", err)
		return err
	}
	param.faster_den = binary.BigEndian.Uint16(twoBytes)

	/* try to find the file statistics */
	info, _ := session.transfer.file.Stat()
	param.file_size = uint64(info.Size())
	var last uint64 = 1
	if param.file_size%uint64(param.block_size) == 0 {
		last = 0
	}
	param.block_count = uint32((param.file_size / uint64(param.block_size)) + last)
	param.epoch = time.Now()

	/* reply with the length, block size, number of blocks, and run epoch */
	eightBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(eightBytes, param.file_size)
	_, err = session.client_fd.Write(eightBytes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not submit file size", err)
		return err
	}

	binary.BigEndian.PutUint32(fourBytes, param.block_size)
	_, err = session.client_fd.Write(fourBytes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not submit block size", err)
		return err
	}

	binary.BigEndian.PutUint32(fourBytes, param.block_count)
	_, err = session.client_fd.Write(fourBytes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not submit block count", err)
		return err
	}

	epoch := param.epoch.Unix()

	binary.BigEndian.PutUint32(fourBytes, uint32(epoch))
	_, err = session.client_fd.Write(fourBytes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not submit run epoch", err)
		return err
	}

	/*calculate and convert RTT to u_sec*/
	param.wait_u_sec = uint32(tEnd.Sub(tStart).Nanoseconds() / 1000)
	/*add a 10% safety margin*/
	param.wait_u_sec = param.wait_u_sec + uint32(float32(param.wait_u_sec)*0.1)

	/* and store the inter-packet delay */
	param.ipd_time = uint32(1000000 * 8 * int64(param.block_size) / int64(param.target_rate))
	session.transfer.ipd_current = float64(param.ipd_time * 3)

	session.XsriptOpen()

	return nil
}

func (session *Session) Transfer() {
	datagram := make([]byte, tsunami.MAX_BLOCK_SIZE+6)
	start := time.Now()
	session.XsriptDataStart(start)

	lasthblostreport := start
	lastfeedback := start
	prevpacketT := start
	var deadconnection_counter uint32
	var ipd_time int64
	var ipd_time_max int64
	var ipd_usleep_diff int64
	retransmitlen := 0
	xfer := session.transfer
	param := session.parameter
	/* start by blasting out every block */
	xfer.block = 0
	retransmission_byte := make([]byte, tsunami.SIZE_OF_RETRANSMISSION_T)
	var retransmission *tsunami.Retransmission
	for xfer.block <= param.block_count {
		/* default: flag as retransmitted block */
		block_type := tsunami.TS_BLOCK_RETRANSMISSION
		/* precalculate time to wait after sending the next packet */
		currpacketT := time.Now()
		//? old -new time
		ipd_usleep_diff = int64(xfer.ipd_current + float64(prevpacketT.Sub(currpacketT).Nanoseconds()/1000))
		prevpacketT = currpacketT

		if ipd_usleep_diff > 0 || ipd_time > 0 {
			ipd_time += ipd_usleep_diff
		}
		if ipd_time > ipd_time_max {
			ipd_time_max = ipd_time
		}

		/* see if transmit requests are available */
		read, err := session.client_fd.Read(retransmission_byte[retransmitlen:])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Retransmission read failed", err)
			return
		}
		retransmitlen += read

		/* if we have a retransmission */
		if retransmitlen == tsunami.SIZE_OF_RETRANSMISSION_T {
			/* store current time */
			lastfeedback = currpacketT
			lasthblostreport = currpacketT
			deadconnection_counter = 0
			retransmission = tsunami.NewRetransmission(retransmission_byte)
			if retransmission.RequestType == tsunami.REQUEST_STOP {
				fmt.Fprintf(os.Stderr, "Transmission of %s complete.\n", xfer.filename)
				param.FinishHook(xfer.filename)
				break
			}
			/* otherwise, handle the retransmission */
			err = session.AcceptRetransmit(*retransmission, datagram)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Retransmission error", err)
			}
			retransmitlen = 0
			/* if we have no retransmission */
		} else if retransmitlen < tsunami.SIZE_OF_RETRANSMISSION_T {
			/* build the block */
			xfer.block = xfer.block + 1
			if xfer.block > param.block_count {
				xfer.block = param.block_count
			}
			if xfer.block == param.block_count {
				block_type = tsunami.TS_BLOCK_TERMINATE
			} else {
				block_type = tsunami.TS_BLOCK_ORIGINAL
			}
			err = session.buildDatagram(xfer.block, uint16(block_type), datagram)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Could not read block", xfer.block, err)
				return
			}
			_, err = xfer.udp_fd.WriteTo(datagram[:6+param.block_size], xfer.udp_address)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Could not transmit block", xfer.block, err)
				continue
			}
			/* if we have too long retransmission message */
		} else if retransmitlen > tsunami.SIZE_OF_RETRANSMISSION_T {
			fmt.Fprintln(os.Stderr, "warn: retransmitlen >", tsunami.SIZE_OF_RETRANSMISSION_T)
			retransmitlen = 0
		}
		/* monitor client heartbeat and disconnect dead client */
		if deadconnection_counter > 2048 {
			deadconnection_counter = 0

			/* limit 'heartbeat lost' reports to 500ms intervals */
			if time.Since(lasthblostreport).Nanoseconds()/1000 < 500000 {
				continue
			}
			lasthblostreport = time.Now()

			/* throttle IPD with fake 100% loss report */
			retransmission = &tsunami.Retransmission{tsunami.REQUEST_ERROR_RATE, 0, 100000}

			session.AcceptRetransmit(*retransmission, datagram)

			delta := time.Since(lastfeedback).Nanoseconds() / 1000

			/* show an (additional) statistics line */
			status_line := fmt.Sprintf(
				"   n/a     n/a     n/a %7u %6.2f %3u -- no heartbeat since %3.2fs\n",
				xfer.block, 100.0*xfer.block/param.block_count,
				session.session_id, delta/1e6)
			session.XsriptDataLog(status_line)
			fmt.Fprintln(os.Stderr, status_line)

			/* handle timeout for normal file transfers */
			if delta/1e6 > int64(param.hb_timeout) {
				fmt.Fprintf(os.Stderr,
					"Heartbeat timeout of %d seconds reached, terminating transfer.\n",
					param.hb_timeout)
				break
			}

		} else {
			deadconnection_counter++
		}

		/* wait before handling the next packet */
		if block_type == tsunami.TS_BLOCK_TERMINATE {
			time.Sleep(time.Duration(10 * ipd_time_max * 1000))
		}
		if ipd_time > 0 {
			time.Sleep(time.Duration(ipd_time * 1000))
		}
	}

	/*---------------------------
	 * STOP TIMING
	 *---------------------------*/
	stop := time.Now()
	session.XsriptDataStop(stop)

	delta := uint64(stop.Sub(start).Nanoseconds() / 1000)

	if param.verbose {
		fmt.Fprintf(os.Stderr,
			"Server %d transferred %llu bytes in %0.2f seconds (%0.1f Mbps)\n",
			session.session_id, param.file_size, delta/1000000.0,
			8.0*param.file_size*1e6/(delta*1024*1024))
	}
	session.XsriptClose(delta)
	xfer.file.Close()
	xfer.udp_fd.Close()
	session.transfer = &Transfer{}
}
