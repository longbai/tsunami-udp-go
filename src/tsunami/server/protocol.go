package server

import (
	"errors"
	"fmt"
	// "io"
	"crypto/rand"
	"encoding/binary"
	"net"
	"os"
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
}

func NewSession(id uint32, conn *net.TCPConn, param *Parameter) *Session {
	session := &Session{}
	session.transfer = &Transfer{}
	session.session_id = id
	session.client_fd = conn
	session.parameter = param
	return session
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
var iteration = 0

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
	if address.String() == "" {
		return nil
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

/*------------------------------------------------------------------------
 * int ttp_open_transfer(ttp_session_t *session);
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
	return nil
}

// int ttp_open_transfer(ttp_session_t *session)
// {
//     char             filename[MAX_FILENAME_LENGTH];  /* the name of the file to transfer     */
//     u_int64_t        file_size;                      /* network-order version of file size   */
//     u_int32_t        block_size;                     /* network-order version of block size  */
//     u_int32_t        block_count;                    /* network-order version of block count */
//     time_t           epoch;
//     int              status;
//     ttp_transfer_t  *xfer  = &session->transfer;
//     ttp_parameter_t *param =  session->parameter;

//     char       size[10];
//     char       file_no[10];
//     char       message[20];
//     u_int16_t  i;
//     struct     timeval ping_s, ping_e;

//     /* clear out the transfer data */
//     memset(xfer, 0, sizeof(*xfer));

//     /* read in the requested filename */
//     status = read_line(session->client_fd, filename, MAX_FILENAME_LENGTH);
//     if (status < 0)
//         error("Could not read filename from client");
//     filename[MAX_FILENAME_LENGTH - 1] = '\0';

//     if(!strcmp(filename, TS_DIRLIST_HACK_CMD)) {

//        /* The client requested listing of files and their sizes (dir command)
//         * Send strings:   NNN \0   name1 \0 len1 \0     nameN \0 lenN \0
//         */
//         snprintf(file_no, sizeof(file_no), "%u", param->total_files);
//         full_write(session->client_fd, file_no, strlen(file_no)+1);
//         for(i=0; i<param->total_files; i++) {
//             full_write(session->client_fd, param->file_names[i], strlen(param->file_names[i])+1);
//             snprintf(message, sizeof(message), "%Lu", (ull_t)(param->file_sizes[i]));
//             full_write(session->client_fd, message, strlen(message)+1);
//         }
//         full_read(session->client_fd, message, 1);
//         return warn("File list sent!");

//     } else if(!strcmp(filename,"*")) {

//         if(param->allhook != 0)
//         {
//            /* execute the provided program on server side to see what files
//             * should be gotten
//             */
// 			const int MaxFileListLength = 32768;
// 			char fileList[MaxFileListLength];
// 			const char *fl;
// 			int nFile = 0;
// 			int length = 0;
// 			int l;
//             FILE *p;

//             fprintf(stderr, "Using allhook program: %s\n", param->allhook);
//             p = popen((char *)(param->allhook), "r");
//             if(p)
//             {

//                 memset(fileList, 0, MaxFileListLength);

//                 while(1)
//                 {
//                     if(fgets(message, sizeof(message), p) == 0)
//                         break;

//                     /* get lenght of string and strip non printable chars */
//                     for(l = 0; message[l] >= ' '; ++l) {}
//                     message[l] = 0;

//                     fprintf(stdout, "  '%s'\n", message);

//                     if(l + length >= MaxFileListLength)
//                         break;

//                     strncpy(fileList + length, message, l);
//                     length += l + 1;
//                     ++nFile;
//                 }
//             }
//             pclose(p);

//             memset(size, 0, sizeof(size));
//             snprintf(size, sizeof(size), "%u", length);
//             full_write(session->client_fd, size, 10);

//             memset(file_no, 0, sizeof(file_no));
//             snprintf(file_no, sizeof(file_no), "%u", nFile);
//             full_write(session->client_fd, file_no, 10);

//             printf("\nSent multi-GET filename count and array size to client\n");
//             memset(message, 0, sizeof(message));
//             full_read(session->client_fd, message, 8);
//             printf("Client response: %s\n", message);

//             fl = fileList;
//             if(nFile > 0)
//             {
//                 for(i=0; i<nFile; i++)
//                 {
//                     l = strlen(fl);
//                     full_write(session->client_fd, fl, l+1);
//                     fl += l+1;
//                 }

//                 memset(message, 0, sizeof(message));
//                 full_read(session->client_fd, message, 8);
//                 printf("Sent file list, client response: %s\n", message);

//                 status = read_line(session->client_fd, filename, MAX_FILENAME_LENGTH);

//                 if (status < 0)
//                     error("Could not read filename from client");
//             }

//         } else {

//            /* A multiple file request - sent the file names first,
//             * and next the client requests a download of each in turn (get * command)
//             */
//             memset(size, 0, sizeof(size));
//             snprintf(size, sizeof(size), "%u", param->file_name_size);
//             full_write(session->client_fd, size, 10);

//             memset(file_no, 0, sizeof(file_no));
//             snprintf(file_no, sizeof(file_no), "%u", param->total_files);
//             full_write(session->client_fd, file_no, 10);

//             printf("\nSent multi-GET filename count and array size to client\n");
//             memset(message, 0, sizeof(message));
//             full_read(session->client_fd, message, 8);
//             printf("Client response: %s\n", message);

//             for(i=0; i<param->total_files; i++)
//                 full_write(session->client_fd, param->file_names[i], strlen(param->file_names[i])+1);

//             memset(message, 0, sizeof(message));
//             full_read(session->client_fd, message, 8);
//             printf("Sent file list, client response: %s\n", message);

//             status = read_line(session->client_fd, filename, MAX_FILENAME_LENGTH);

//             if (status < 0)
//                 error("Could not read filename from client");
//         }
//     }

//     /* store the filename in the transfer object */
//     xfer->filename = strdup(filename);
//     if (xfer->filename == NULL)
//         return warn("Memory allocation error");

//     /* make a note of the request */
//     if (param->verbose_yn)
//         printf("Request for file: '%s'\n", filename);

//     #ifndef VSIB_REALTIME

//     /* try to open the file for reading */
//     xfer->file = fopen(filename, "r");
//     if (xfer->file == NULL) {
//         sprintf(g_error, "File '%s' does not exist or cannot be read", filename);
//         /* signal failure to the client */
//         status = full_write(session->client_fd, "\x008", 1);
//         if (status < 0)
//             warn("Could not signal request failure to client");
//         return warn(g_error);
//     }

//     #else

//     /* get starting time (UTC) and detect whether local disk copy is wanted */
//     if (strrchr(filename,'/') == NULL) {
//         ef = parse_evn_filename(filename);          /* attempt to parse */
//         param->fileout = 0;
//     } else {
//         ef = parse_evn_filename(strrchr(filename, '/')+1);       /* attempt to parse */
//         param->fileout = 1;
//     }
//     if (!ef->valid) {
//         fprintf(stderr, "Warning: EVN filename parsing failed, '%s' not following EVN File Naming Convention?\n", filename);
//     }

//     /* get time multiplexing info from EVN filename (currently these are all unused) */
//     if (get_aux_entry("sl",ef->auxinfo, ef->nr_auxinfo) == 0)
//       param->totalslots= 1;          /* default to 1 */
//     else
//       sscanf(get_aux_entry("sl",ef->auxinfo, ef->nr_auxinfo), "%d", &(param->totalslots));

//     if (get_aux_entry("sn",ef->auxinfo, ef->nr_auxinfo) == 0)
//       param->slotnumber= 1;          /* default to 1 */
//     else
//       sscanf(get_aux_entry("sn",ef->auxinfo, ef->nr_auxinfo), "%d", &param->slotnumber);

//     if (get_aux_entry("sr",ef->auxinfo, ef->nr_auxinfo) == 0)
//       param->samplerate= 512;          /* default to 512 Msamples/s */
//     else
//       sscanf(get_aux_entry("sr",ef->auxinfo, ef->nr_auxinfo), "%d", &param->samplerate);

//     /* try to open the vsib for reading */
//     xfer->vsib = fopen("/dev/vsib", "r");
//     if (xfer->vsib == NULL) {
//         sprintf(g_error, "VSIB board does not exist in /dev/vsib or it cannot be read");
//         status = full_write(session->client_fd, "\002", 1);
//         if (status < 0) {
//             warn("Could not signal request failure to client");
//         }
//         return warn(g_error);
//     }

//     /* try to open the local disk copy file for writing */
//     if (param->fileout) {
//         xfer->file = fopen(filename, "wb");
//         if (xfer->file == NULL) {
//             sprintf(g_error, "Could not open local file '%s' for writing", filename);
//             status = full_write(session->client_fd, "\x010", 1);
//             if (status < 0) {
//                 warn("Could not signal request failure to client");
//             }
//             fclose(xfer->vsib);
//             return warn(g_error);
//         }
//     }

//     /* Start half a second before full UTC seconds change. If EVN filename date/time parse failed, start immediately. */
//     if (!(NULL == ef->data_start_time_ascii || ef->data_start_time <= 1.0)) {
//         u_int64_t timedelta_usec;
//         starttime = ef->data_start_time - 0.5;

//         assert( gettimeofday(&d, NULL) == 0 );
//         timedelta_usec = (unsigned long)((starttime - (double)d.tv_sec)* 1000000.0) - (double)d.tv_usec;
//         fprintf(stderr, "Sleeping until specified time (%s) for %Lu usec...\n", ef->data_start_time_ascii, (ull_t)timedelta_usec);
//         usleep_that_works(timedelta_usec);
//     }

//     /* Check if the client is still connected after the long(?) wait */
//     //if(recv(session->client_fd, &status, 1, MSG_PEEK)<0) {
//     //    // connection has terminated, exit
//     //    fclose(xfer->vsib);
//     //    return warn("The client disconnected while server was sleeping.");
//     //}

//     /* start at next 1PPS pulse */
//     start_vsib(session);

//     #endif // end of VSIB_REALTIME section

//     /* begin round trip time estimation */
//     gettimeofday(&ping_s,NULL);

//     /* try to signal success to the client */
//     status = full_write(session->client_fd, "\000", 1);
//     if (status < 0)
//         return warn("Could not signal request approval to client");

//     /* read in the block size, target bitrate, and error rate */
//     if (full_read(session->client_fd, &param->block_size,  4) < 0) return warn("Could not read block size");            param->block_size  = ntohl(param->block_size);
//     if (full_read(session->client_fd, &param->target_rate, 4) < 0) return warn("Could not read target bitrate");        param->target_rate = ntohl(param->target_rate);
//     if (full_read(session->client_fd, &param->error_rate,  4) < 0) return warn("Could not read error rate");            param->error_rate  = ntohl(param->error_rate);

//     /* end round trip time estimation */
//     gettimeofday(&ping_e,NULL);

//     /* read in the slowdown and speedup factors */
//     if (full_read(session->client_fd, &param->slower_num,  2) < 0) return warn("Could not read slowdown numerator");    param->slower_num  = ntohs(param->slower_num);
//     if (full_read(session->client_fd, &param->slower_den,  2) < 0) return warn("Could not read slowdown denominator");  param->slower_den  = ntohs(param->slower_den);
//     if (full_read(session->client_fd, &param->faster_num,  2) < 0) return warn("Could not read speedup numerator");     param->faster_num  = ntohs(param->faster_num);
//     if (full_read(session->client_fd, &param->faster_den,  2) < 0) return warn("Could not read speedup denominator");   param->faster_den  = ntohs(param->faster_den);

//     #ifndef VSIB_REALTIME
//     /* try to find the file statistics */
//     fseeko(xfer->file, 0, SEEK_END);
//     param->file_size   = ftello(xfer->file);
//     fseeko(xfer->file, 0, SEEK_SET);
//     #else
//     /* get length of recording in bytes from filename */
//     if (get_aux_entry("flen", ef->auxinfo, ef->nr_auxinfo) != 0) {
//         sscanf(get_aux_entry("flen", ef->auxinfo, ef->nr_auxinfo), "%" SCNu64, (u_int64_t*) &(param->file_size));
//     } else if (get_aux_entry("dl", ef->auxinfo, ef->nr_auxinfo) != 0) {
//         sscanf(get_aux_entry("dl", ef->auxinfo, ef->nr_auxinfo), "%" SCNu64, (u_int64_t*) &(param->file_size));
//     } else {
//         param->file_size = 60LL * 512000000LL * 4LL / 8; /* default to amount of bytes equivalent to 4 minutes at 512Mbps */
//     }
//     fprintf(stderr, "Realtime file length in bytes: %Lu\n", (ull_t)param->file_size);
//     #endif

//     param->block_count = (param->file_size / param->block_size) + ((param->file_size % param->block_size) != 0);
//     param->epoch       = time(NULL);

//     /* reply with the length, block size, number of blocks, and run epoch */
//     file_size   = htonll(param->file_size);    if (full_write(session->client_fd, &file_size,   8) < 0) return warn("Could not submit file size");
//     block_size  = htonl (param->block_size);   if (full_write(session->client_fd, &block_size,  4) < 0) return warn("Could not submit block size");
//     block_count = htonl (param->block_count);  if (full_write(session->client_fd, &block_count, 4) < 0) return warn("Could not submit block count");
//     epoch       = htonl (param->epoch);        if (full_write(session->client_fd, &epoch,       4) < 0) return warn("Could not submit run epoch");

//     /*calculate and convert RTT to u_sec*/
//     session->parameter->wait_u_sec=(ping_e.tv_sec - ping_s.tv_sec)*1000000+(ping_e.tv_usec-ping_s.tv_usec);
//     /*add a 10% safety margin*/
//     session->parameter->wait_u_sec = session->parameter->wait_u_sec + ((int)(session->parameter->wait_u_sec* 0.1));

//     /* and store the inter-packet delay */
//     param->ipd_time   = (u_int32_t) ((1000000LL * 8 * param->block_size) / param->target_rate);
//     xfer->ipd_current = param->ipd_time * 3;

//     /* if we're doing a transcript */
//     if (param->transcript_yn)
//         xscript_open(session);

//     /* we succeeded! */
//     return 0;
// }

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
			_, err = xfer.udp_fd.WriteTo(datagram, xfer.udp_address)
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
