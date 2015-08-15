package client

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"

	"tsunami"
)

/*------------------------------------------------------------------------
 * int Command_close(Command_t *Command, ttp_session_t *session)
 *
 * Closes the given open Tsunami control session if it's active, thus
 * making it invalid for further use.  Returns 0 on success and non-zero
 * on error.
 *------------------------------------------------------------------------*/
func CommandClose(session *Session) error {
	if session == nil {
		return nil
	}
	session.connection.Close()
	session.connection = nil
	fmt.Println("Connection closed")
	return nil
}

/*------------------------------------------------------------------------
 * ttp_session_t *Command_connect(Command_t *Command,
 *                                ttp_parameter_t *parameter)
 *
 * Opens a new Tsunami control session to the server specified in the
 * command or in the given set of default parameters.  This involves
 * prompting the user to enter the shared secret.  On success, we return
 * a pointer to the new TTP session object.  On failure, we return NULL.
 *
 * Note that the default host and port stored in the parameter object
 * are updated if they were specified in the command itself.
 *------------------------------------------------------------------------*/
func CommandConnect(parameter *Parameter, args []string) (session *Session, err error) {
	if len(args) >= 1 && args[0] != "" {
		parameter.serverName = args[0]
	}

	/* if we were given a port, store that information */
	if len(args) >= 2 {
		port, err1 := strconv.ParseInt(args[1], 10, 32)
		if err1 != nil {
			return nil, err1
		}
		parameter.serverPort = uint16(port)
	}
	/* allocate a new session */
	session = new(Session)
	session.param = *parameter
	server := fmt.Sprintf("%v:%v", parameter.serverName, parameter.serverPort)

	session.connection, err = net.Dial("tcp", server)
	if err != nil {
		return nil, err
	}

	err = session.ttp_negotiate()
	if err != nil {
		session.connection.Close()
		session.connection = nil
		return nil, err
	}

	secret := tsunami.DEFAULT_SECRET
	if parameter.passphrase != "" {
		secret = parameter.passphrase
	}

	err = session.ttp_authenticate(secret)
	if err != nil {
		session.connection.Close()
		session.connection = nil
		return nil, err
	}

	if session.param.verbose {
		fmt.Println("Connected\n")
	}

	return session, nil
}

/*------------------------------------------------------------------------
 * int Command_dir(Command_t *Command, ttp_session_t *session)
 *
 * Tries to request a list of server shared files and their sizes.
 * Returns 0 on a successful transfer and nonzero on an error condition.
 * Allocates and fills out session->fileslist struct, the caller needs to
 * free it after use.
 *------------------------------------------------------------------------*/
func CommandDir(session *Session) error {

	if session == nil || session.connection == nil {
		return errors.New("Not connected to a Tsunami server")
	}
	data := []byte(fmt.Sprintf("%v\n", tsunami.TS_DIRLIST_HACK_CMD))
	session.connection.Write(data)
	result := make([]byte, 1)
	_, err := session.connection.Read(result)
	if err != nil {
		return err
	}
	if result[0] == 8 {
		return errors.New("Server does no support listing of shared files")
	}

	str, err := tsunami.ReadLine(session.connection, 2048)
	if err != nil {
		return err
	}
	var numOfFile int64
	str = fmt.Sprint(string(result), str)
	if str != "" {
		numOfFile, err = strconv.ParseInt(str, 10, 32)
		if err != nil {
			return err
		}
	}

	fmt.Fprintln(os.Stderr, "Remote file list:")
	for i := 0; i < int(numOfFile); i++ {
		str, _ = tsunami.ReadLine(session.connection, 2048)
		fmt.Fprintf(os.Stderr, "%v) %v\t", i+1, str)
		str, _ = tsunami.ReadLine(session.connection, 2048)
		fmt.Fprintf(os.Stderr, "%v bytes\n", str)
	}
	fmt.Fprintln(os.Stderr, "")
	session.connection.Write([]byte{'0'})
	return nil
}

/*------------------------------------------------------------------------
 * int Command_get(Command_t *Command, ttp_session_t *session)
 *
 * Tries to initiate a file transfer for the remote file given in the
 * command.  If the user did not supply a local filename, we derive it
 * from the remote filename.  Returns 0 on a successful transfer and
 * nonzero on an error condition.
 *------------------------------------------------------------------------*/
func CommandGet(path string, session *Session) error {
	return nil
}

//     u_char         *datagram = NULL;            /* the buffer (in ring) for incoming blocks       */
//     u_char         *local_datagram = NULL;      /* the local temp space for incoming block        */
//     u_int32_t       this_block = 0;             /* the block number for the block just received   */
//     u_int16_t       this_type = 0;              /* the block type for the block just received     */
//     u_int64_t       delta = 0;                  /* generic holder of elapsed times                */
//     u_int32_t       block = 0;                  /* generic holder of a block number               */
//     u_int32_t       dumpcount = 0;

//     double          mbit_thru, mbit_good;       /* helpers for final statistics                   */
//     double          mbit_file;
//     double          time_secs;

//     ttp_transfer_t *xfer          = &(session->transfer)
//     retransmit_t   *rexmit        = &(session->transfer.retransmit)
//     int             status = 0;
//     pthread_t       disk_thread_id = 0;

//     /* The following variables will be used only in multiple file transfer
//      * session they are used to recieve the file names and other parameters
//      */
//     int             multimode = 0;
//     char          **file_names = NULL;
//     u_int32_t       f_counter = 0, f_total = 0, f_arrsize = 0;

//     /* this struct wil hold the RTT time */
//     struct timeval ping_s, ping_e;
//     long wait_u_sec = 1;

//     /* make sure that we have a remote file name */
//     if (command->count < 2)
//     return warn("Invalid command syntax (use 'help get' for details)");

//     /* make sure that we have an open session */
//     if (session == NULL || session->server == NULL)
//     return warn("Not connected to a Tsunami server");

//     /* reinitialize the transfer data */
//     memset(xfer, 0, sizeof(*xfer));

//     /* if the client asking for multiple files to be transfered */
//     if(!strcmp("*",command->text[1])) {
//        char  filearray_size[10];
//        char  file_count[10];

//        multimode = 1;
//        printf("Requesting all available files\n");

//        /* Send request and try to calculate the RTT from client to server */
//        gettimeofday(&(ping_s), NULL);
//        status = fprintf(session->server, "%s\n", command->text[1]);
//        status = fread(filearray_size, sizeof(char), 10, session->server);
//        gettimeofday(&(ping_e),NULL);

//        status = fread(file_count, sizeof(char), 10, session->server);
//        fprintf(session->server, "got size");

//        if ((status <= 0) || fflush(session->server))
//           return warn("Could not request file");

//        /* See if the request was successful */
//        if (status < 1)
//           return warn("Could not read response to file request");

//        /* Calculate and convert RTT to u_sec, with +10% margin */
//        wait_u_sec = (ping_e.tv_sec - ping_s.tv_sec)*1000000+(ping_e.tv_usec-ping_s.tv_usec);
//        wait_u_sec = wait_u_sec + ((int)(wait_u_sec* 0.1));

//        /* Parse */
//        sscanf(filearray_size, "%u", &f_arrsize);
//        sscanf(file_count, "%u", &f_total);

//        if (f_total <= 0) {
//           /* get the \x008 failure signal */
//           char dummy[1];
//           status = fread(dummy, sizeof(char), 1, session->server);

//           return warn("Server advertised no files to get");
//        }
//        else
//        {
//           printf("\nServer is sharing %u files\n", f_total);

//           /* Read the file list */
//           file_names = malloc(f_total * sizeof(char*));
//           if(file_names == NULL)
//              error("Could not allocate memory\n");

//           printf("Multi-GET of %d files:\n", f_total);
//           for(f_counter=0; f_counter<f_total; f_counter++) {
//              char tmpname[1024];
//              fread_line(session->server, tmpname, 1024);
//              file_names[f_counter] = strdup(tmpname);
//              printf("%s ", file_names[f_counter]);
//           }
//           fprintf(session->server, "got list");
//           printf("\n");
//        }

//     } else {
//        f_total = 1;
//     }

//     f_counter = 0;
//     do /*---loop for single and multi file request---*/
//     {

//     /* store the remote filename */
//     if(!multimode)
//        xfer->remote_filename = command->text[1];
//     else
//        xfer->remote_filename = file_names[f_counter];

//     /* store the local filename */
//     if(!multimode) {
//        if (command->count >= 3) {
//           /* command was in "GET remotefile localfile" style */
//           xfer->local_filename = command->text[2];
//        } else {
//           /* trim into local filename without '/' */
//           xfer->local_filename = strrchr(command->text[1], '/');
//           if (xfer->local_filename == NULL)
//              xfer->local_filename = command->text[1];
//           else
//              ++(xfer->local_filename);
//        }
//     } else {
//        /* don't trim, GET* writes into remotefilename dir if exists, otherwise into CWD */
//        xfer->local_filename = file_names[f_counter];
//        printf("GET *: now requesting file '%s'\n", xfer->local_filename);
//     }

//     /* negotiate the file request with the server */
//     if (ttp_open_transfer(session, xfer->remote_filename, xfer->local_filename) < 0)
//     return warn("File transfer request failed");

//     /* create the UDP data socket */
//     if (ttp_open_port(session) < 0)
//     return warn("Creation of data socket failed");

//     /* allocate the retransmission table */
//     rexmit->table = (u_int32_t *) calloc(DEFAULT_TABLE_SIZE, sizeof(u_int32_t));
//     if (rexmit->table == NULL)
//     error("Could not allocate retransmission table");

//     /* allocate the received bitfield */
//     xfer->received = (u_char *) calloc(xfer->block_count / 8 + 2, sizeof(u_char));
//     if (xfer->received == NULL)
//     error("Could not allocate received-data bitfield");

//     /* allocate the ring buffer */
//     xfer->ring_buffer = ring_create(session);

//     /* allocate the faster local buffer */
//     local_datagram = (u_char *) calloc(6 + session->parameter->block_size, sizeof(u_char));
//     if (local_datagram == NULL)
//         error("Could not allocate fast local datagram buffer in Command_get()");

//     /* start up the disk I/O thread */
//     status = pthread_create(&disk_thread_id, NULL, disk_thread, session);
//     if (status != 0)
//     error("Could not create I/O thread");

//     /* Finish initializing the retransmission object */
//     rexmit->table_size = DEFAULT_TABLE_SIZE;
//     rexmit->index_max  = 0;

//     /* we start by expecting block #1 */
//     xfer->next_block = 1;
//     xfer->gapless_to_block = 0;

//    /*---------------------------
//    * START TIMING
//    *---------------------------*/

//    memset(&xfer->stats, 0, sizeof(xfer->stats));
//    xfer->stats.start_udp_errors = get_udp_in_errors();
//    xfer->stats.this_udp_errors = xfer->stats.start_udp_errors;
//    gettimeofday(&(xfer->stats.start_time), NULL);
//    gettimeofday(&(xfer->stats.this_time),  NULL);
//    if (session->parameter->transcript_yn)
//       xscript_data_start(session, &(xfer->stats.start_time));

//    /* until we break out of the transfer */
//    while (1) {

//       /* try to receive a datagram */
//       status = recvfrom(xfer->udp_fd, local_datagram, 6 + session->parameter->block_size, 0, NULL, 0);
//       if (status < 0) {
//           warn("UDP data transmission error");
//           printf("Apparently frozen transfer, trying to do retransmit request\n");
//           if (ttp_repeat_retransmit(session) < 0) {  /* repeat our requests */
//              warn("Repeat of retransmission requests failed");
//              goto abort;
//           }
//       }

//       /* retrieve the block number and block type */
//       this_block = ntohl(*((u_int32_t *) local_datagram));       // in range of 1..xfer->block_count
//       this_type  = ntohs(*((u_int16_t *) (local_datagram + 4))); // TS_BLOCK_ORIGINAL etc

//       /* keep statistics on received blocks */
//       xfer->stats.total_blocks++;
//       if (this_type != TS_BLOCK_RETRANSMISSION) {
//           xfer->stats.this_flow_originals++;
//       } else {
//           xfer->stats.this_flow_retransmitteds++;
//           xfer->stats.total_recvd_retransmits++;
//       }

//       /* main transfer control logic */
//       if (!ring_full(xfer->ring_buffer)) /* don't let disk-I/O freeze stop feedback of stats to server */
//       if (!got_block(session, this_block) || this_type == TS_BLOCK_TERMINATE || xfer->restart_pending)
//       {

//           /* insert new blocks into disk write ringbuffer */
//           if (!got_block(session, this_block)) {

//               /* reserve ring space, copy the data in, confirm the reservation */
//               datagram = ring_reserve(xfer->ring_buffer);
//               memcpy(datagram, local_datagram, 6 + session->parameter->block_size);
//               if (ring_confirm(xfer->ring_buffer) < 0) {
//                   warn("Error in accepting block");
//                   goto abort;
//               }

//               /* mark the block as received */
//               xfer->received[this_block / 8] |= (1 << (this_block % 8));
//               if (xfer->blocks_left > 0) {
//                   --(xfer->blocks_left);
//               } else {
//                   printf("Oops! Negative-going blocks_left count at block: type=%c this=%u final=%u left=%u\n", this_type, this_block, xfer->block_count, xfer->blocks_left);
//               }
//           }

//           /* transmit restart: avoid re-triggering on blocks still down the wire before server reacts */
//           if ((xfer->restart_pending) && (this_type != TS_BLOCK_TERMINATE)) {
//               if ((this_block > xfer->restart_lastidx) && (this_block <= xfer->restart_wireclearidx)) {
//                   goto send_stats;
//               }
//           }

//           /* queue any retransmits we need */
//           if (this_block > xfer->next_block) {

//              /* lossy transfer mode */
//              if (!session->parameter->lossless) {
//                 if (session->parameter->losswindow_ms == 0) {
//                     /* lossy transfer, no retransmits */
//                     xfer->gapless_to_block = this_block;
//                 } else {
//                     /* semi-lossy transfer, purge data past specified approximate time window */
//                     double path_capability;
//                     path_capability  = 0.8 * (xfer->stats.this_transmit_rate + xfer->stats.this_retransmit_rate); // reduced effective Mbit/s rate
//                     path_capability *= (0.001 * session->parameter->losswindow_ms); // MBit inside window, round-trip user estimated in losswindow_ms!
//                     u_int32_t earliest_block = this_block -
//                        min(
//                          1024 * 1024 * path_capability / (8 * session->parameter->block_size),  // # of blocks inside window
//                          (this_block - xfer->gapless_to_block)                                  // # of blocks missing (tops)
//                        );
//                     for (block = earliest_block; block < this_block; ++block) {
//                         if (ttp_request_retransmit(session, block) < 0) {
//                             warn("Retransmission request failed");
//                             goto abort;
//                         }
//                     }
//                     // hop over the missing section
//                     xfer->next_block = earliest_block;
//                     xfer->gapless_to_block = earliest_block;
//                 }

//              /* lossless transfer mode, request all missing data to be resent */
//              } else {
//                 for (block = xfer->next_block; block < this_block; ++block) {
//                     if (ttp_request_retransmit(session, block) < 0) {
//                         warn("Retransmission request failed");
//                         goto abort;
//                     }
//                 }
//              }
//           }//if(missing blocks)

//           /* advance the index of the gapless section going from start block to highest block  */
//           while (got_block(session, xfer->gapless_to_block + 1) && (xfer->gapless_to_block < xfer->block_count)) {
//               xfer->gapless_to_block++;
//           }

//           /* if this is an orignal, we expect to receive the successor to this block next */
//           /* transmit restart note: these resent blocks are labeled original as well      */
//           if (this_type == TS_BLOCK_ORIGINAL) {
//               xfer->next_block = this_block + 1;
//           }

//           /* transmit restart: already got out of the missing blocks range? */
//           if (xfer->restart_pending && (xfer->next_block >= xfer->restart_lastidx)) {
//               xfer->restart_pending = 0;
//           }

//           /* are we at the end of the transmission? */
//           if (this_type == TS_BLOCK_TERMINATE) {

//               #if DEBUG_RETX
//               fprintf(stderr, "Got end block: blk %u, final blk %u, left blks %u, tail %u, head %u\n",
//                       this_block, xfer->block_count, xfer->blocks_left, xfer->gapless_to_block, xfer->next_block);
//               #endif

//               /* got all blocks by now */
//               if (xfer->blocks_left == 0) {
//                   break;
//               } else if (!session->parameter->lossless) {
//                   if ((rexmit->index_max==0) && !(xfer->restart_pending)) {
//                       break;
//                   }
//               }

//               /* add possible still missing blocks to retransmit list */
//               for (block = xfer->gapless_to_block+1; block < xfer->block_count; ++block) {
//                   if (ttp_request_retransmit(session, block) < 0) {
//                       warn("Retransmission request failed");
//                       goto abort;
//                   }
//               }

//               /* send the retransmit request list again */
//               ttp_repeat_retransmit(session);
//           }

//       }//if(not a duplicate block)

//     send_stats:

//       /* repeat our server feedback and requests if it's time */
//       if (!(xfer->stats.total_blocks % 50)) {

//           /* if it's been at least 350ms */
//           if (get_usec_since(&(xfer->stats.this_time)) > UPDATE_PERIOD) {

//             /* repeat our retransmission requests */
//             if (ttp_repeat_retransmit(session) < 0) {
//                 warn("Repeat of retransmission requests failed");
//                 goto abort;
//             }

//             /* send and show our current statistics */
//             ttp_update_stats(session);

//             /* progress blockmap (DEBUG) */
//             if (session->parameter->blockdump) {
//                 char postfix[64];
//                 snprintf(postfix, 63, ".bmap%u", dumpcount++);
//                 dump_blockmap(postfix, xfer);
//             }
//          }
//       }

//     } /* Transfer of the file completes here*/

//     printf("Transfer complete. Flushing to disk and signaling server to stop...\n");

//     /*---------------------------
//      * STOP TIMING
//      *---------------------------*/

//     /* tell the server to quit transmitting */
//     close(xfer->udp_fd);
//     if (ttp_request_stop(session) < 0) {
//     warn("Could not request end of transfer");
//     goto abort;
//     }

//     /* add a stop block to the ring buffer */
//     datagram = ring_reserve(xfer->ring_buffer);
//     *((u_int32_t *) datagram) = 0;
//     if (ring_confirm(xfer->ring_buffer) < 0)
//     warn("Error in terminating disk thread");

//     /* wait for the disk thread to die */
//     if (pthread_join(disk_thread_id, NULL) < 0)
//     warn("Disk thread terminated with error");

//     /*------------------------------------
//      * MORE TRUE POINT TO STOP TIMING ;-)
//      *-----------------------------------*/
//     // time here would contain the additional delays from the
//     // local disk flush and server xfer shutdown - include or omit?

//     /* get finishing time */
//     gettimeofday(&(xfer->stats.stop_time), NULL);
//     delta = get_usec_since(&(xfer->stats.start_time));

//     /* count the truly lost blocks from the 'received' bitmap table */
//     xfer->stats.total_lost = 0;
//     for (block=1; block <= xfer->block_count; block++) {
//         if (!got_block(session, block)) xfer->stats.total_lost++;
//     }

//     /* display the final results */
//     mbit_thru     = 8.0 * xfer->stats.total_blocks * session->parameter->block_size;
//     mbit_good     = mbit_thru - 8.0 * xfer->stats.total_recvd_retransmits * session->parameter->block_size;
//     mbit_file     = 8.0 * xfer->file_size;
//     mbit_thru    /= (1024.0*1024.0);
//     mbit_good    /= (1024.0*1024.0);
//     mbit_file    /= (1024.0*1024.0);
//     time_secs     = delta / 1e6;
//     printf("PC performance figure : %llu packets dropped (if high this indicates receiving PC overload)\n",
//                                          (ull_t)(xfer->stats.this_udp_errors - xfer->stats.start_udp_errors));
//     printf("Transfer duration     : %0.2f seconds\n", time_secs);
//     printf("Total packet data     : %0.2f Mbit\n", mbit_thru);
//     printf("Goodput data          : %0.2f Mbit\n", mbit_good);
//     printf("File data             : %0.2f Mbit\n", mbit_file);
//     printf("Throughput            : %0.2f Mbps\n", mbit_thru / time_secs);
//     printf("Goodput w/ restarts   : %0.2f Mbps\n", mbit_good / time_secs);
//     printf("Final file rate       : %0.2f Mbps\n", mbit_file / time_secs);
//     printf("Transfer mode         : ");
//     if (session->parameter->lossless) {
//         if (xfer->stats.total_lost == 0) {
//            printf("lossless\n");
//         } else {
//            printf("lossless mode - but lost count=%u > 0, please file a bug report!!\n", xfer->stats.total_lost);
//         }
//     } else {
//         if (session->parameter->losswindow_ms == 0) {
//             printf("lossy\n");
//         } else {
//             printf("semi-lossy, time window %d ms\n", session->parameter->losswindow_ms);
//         }
//         printf("Data blocks lost      : %llu (%.2f%% of data) per user-specified time window constraint\n",
//                   (ull_t)xfer->stats.total_lost, ( 100.0 * xfer->stats.total_lost ) / xfer->block_count );
//     }
//     printf("\n");

//     /* update the transcript */
//     if (session->parameter->transcript_yn) {
//         xscript_data_stop(session, &(xfer->stats.stop_time));
//         xscript_close(session, delta);
//     }

//     /* dump the received packet bitfield to a file, with added filename prefix ".blockmap" */
//     if (session->parameter->blockdump) {
//        dump_blockmap(".blockmap", xfer);
//     }

//     /* close our open files */
//     if (xfer->file     != NULL) { fclose(xfer->file);    xfer->file     = NULL; }

//     /* deallocate memory */
//     ring_destroy(xfer->ring_buffer);
//     if (rexmit->table != NULL)  { free(rexmit->table);   rexmit->table  = NULL; }
//     if (xfer->received != NULL) { free(xfer->received);  xfer->received = NULL; }
//     if (local_datagram != NULL) { free(local_datagram);  local_datagram = NULL; }

//     /* update the target rate */
//     if (session->parameter->rate_adjust) {
//         session->parameter->target_rate = 1.15 * 1e6 * (mbit_file / time_secs);
//         printf("Adjusting target rate to %d Mbps for next transfer.\n", (int)(session->parameter->target_rate/1e6));
//     }

//     /* more files in "GET *" ? */
//     } while(++f_counter<f_total);

//     /* deallocate file list */
//     if(multimode) {
//        for(f_counter=0; f_counter<f_total; f_counter++) {
//            free(file_names[f_counter]);
//        }
//        free(file_names);
//     }

//     /* we succeeded */
//     return 0;

//  abort:
//     fprintf(stderr, "Transfer not successful.  (WARNING: You may need to reconnect.)\n\n");
//     close(xfer->udp_fd);
//     ring_destroy(xfer->ring_buffer);
//     if (xfer->file     != NULL) { fclose(xfer->file);    xfer->file     = NULL; }
//     if (rexmit->table  != NULL) { free(rexmit->table);   rexmit->table  = NULL; }
//     if (xfer->received != NULL) { free(xfer->received);  xfer->received = NULL; }
//     if (local_datagram != NULL) { free(local_datagram);  local_datagram = NULL; }
//     return -1;
// }

/*------------------------------------------------------------------------
 * int command_set(command_t *command, ttp_parameter_t *parameter);
 *
 * Sets a particular parameter to the given value, or simply reports
 * on the current value of one or more fields.  Returns 0 on success
 * and nonzero on failure.
 *------------------------------------------------------------------------*/

func choice2(flag bool, yes string, no string) string {
	if flag {
		return yes
	}
	return no
}

func choice(flag bool) string {
	return choice2(flag, "yes", "no")
}

func showParam(arg string, parameter *Parameter) {
	switch arg {
	case "server":
		fmt.Println("server =", parameter.serverName)
	case "port":
		fmt.Println("port =", parameter.serverPort)
	case "udpport":
		fmt.Println("udpport =", parameter.clientPort)
	case "buffer":
		fmt.Println("buffer =", parameter.udpBuffer)
	case "blocksize":
		fmt.Println("blocksize =", parameter.blockSize)
	case "verbose":
		fmt.Println("verbose =", choice(parameter.verbose))
	case "transcript":
		fmt.Println("transcript =", choice(parameter.transcript))
	case "ip":
		fmt.Println("ip =", choice(parameter.ipv6))
	case "output":
		fmt.Println("output =", choice2(parameter.outputMode == SCREEN_MODE, "screen", "line"))
	case "rate":
		fmt.Println("rate =", parameter.targetRate)
	case "rateadjust":
		fmt.Println("rateadjust =", choice(parameter.rateAdjust))
	case "error":
		fmt.Println("error = ", parameter.errorRate/1000.0)
	case "slowdown":
		fmt.Printf("slowdown = %v/%v\n", parameter.slowerNum, parameter.slowerDen)
	case "speedup":
		fmt.Printf("speedup = %v/%v\n", parameter.fasterNum, parameter.fasterDen)
	case "history":
		fmt.Println("history = ", parameter.history)
	case "lossless":
		fmt.Println("lossless =", choice(parameter.lossless))
	case "losswindow":
		fmt.Printf("losswindow = %v msec\n", parameter.losswindow_ms)
	case "blockdump":
		fmt.Println("blockdump =", choice(parameter.blockDump))
	case "passphrase":
		fmt.Println("passphrase =", choice2(parameter.passphrase == "", "default", "<user-specified>"))
	}
}

func showAllParam(parameter *Parameter) {
	fmt.Println("server =", parameter.serverName)
	fmt.Println("port =", parameter.serverPort)
	fmt.Println("udpport =", parameter.clientPort)
	fmt.Println("buffer =", parameter.udpBuffer)
	fmt.Println("blocksize =", parameter.blockSize)
	fmt.Println("verbose =", choice(parameter.verbose))
	fmt.Println("transcript =", choice(parameter.transcript))
	fmt.Println("ip =", choice(parameter.ipv6))
	fmt.Println("output =", choice2(parameter.outputMode == SCREEN_MODE, "screen", "line"))
	fmt.Println("rate =", parameter.targetRate)
	fmt.Println("rateadjust =", choice(parameter.rateAdjust))
	fmt.Println("error = ", parameter.errorRate/1000.0)
	fmt.Printf("slowdown = %v/%v\n", parameter.slowerNum, parameter.slowerDen)
	fmt.Printf("speedup = %v/%v\n", parameter.fasterNum, parameter.fasterDen)
	fmt.Println("history = ", parameter.history)
	fmt.Println("lossless =", choice(parameter.lossless))
	fmt.Printf("losswindow = %v msec\n", parameter.losswindow_ms)
	fmt.Println("blockdump =", choice(parameter.blockDump))
	fmt.Println("passphrase =", choice2(parameter.passphrase == "", "default", "<user-specified>"))
}

func CommandSet(parameter *Parameter, args []string) error {
	if len(args) == 1 {
		showParam(args[0], parameter)
	}
	if len(args) == 0 {
		showAllParam(parameter)
	}
	if len(args) == 2 {
		key := args[0]
		value := args[1]
		switch key {
		case "server":
			fmt.Println(key, value)
			parameter.serverName = value
		case "port":
			x, _ := strconv.ParseUint(value, 10, 32)
			parameter.serverPort = uint16(x)
		case "udpport":
			x, _ := strconv.ParseUint(value, 10, 32)
			parameter.clientPort = uint16(x)
		case "buffer":
			x, _ := strconv.ParseUint(value, 10, 32)
			parameter.udpBuffer = uint32(x)
		case "blocksize":
			x, _ := strconv.ParseUint(value, 10, 32)
			parameter.blockSize = uint32(x)
		case "verbose":
			parameter.verbose = (value == "yes")
		case "transcript":
			parameter.transcript = (value == "yes")
		case "ip":
			parameter.ipv6 = (value == "v6")
		case "output":
			if value == "screen" {
				parameter.outputMode = SCREEN_MODE
			} else {
				parameter.outputMode = LINE_MODE
			}
		case "rateadjust":
			parameter.rateAdjust = (value == "yes")
		case "rate":
			multiplier := 1
			v := []byte(value)
			length := len(v)
			if length > 1 {
				last := v[length-1]
				if last == 'G' || last == 'g' {
					multiplier = 1000000000
					v = v[:length-1]
				} else if last == 'M' || last == 'm' {
					multiplier = 1000000
					v = v[:length-1]
				}
			}
			x, _ := strconv.ParseUint(string(v), 10, 64)
			parameter.targetRate = uint32(multiplier) * uint32(x)
		case "error":
			x, _ := strconv.ParseFloat(value, 64)
			parameter.errorRate = uint32(x * 1000.0)
		case "slowdown":
			x, y := tsunami.ParseFraction(value)
			parameter.slowerNum, parameter.slowerDen = uint16(x), uint16(y)
		case "speedup":
			x, y := tsunami.ParseFraction(value)
			parameter.fasterNum, parameter.fasterDen = uint16(x), uint16(y)
		case "history":
			x, _ := strconv.ParseUint(value, 10, 32)
			parameter.history = uint16(x)
		case "lossless":
			parameter.lossless = (value == "yes")
		case "losswindow":
			x, _ := strconv.ParseUint(value, 10, 32)
			parameter.losswindow_ms = uint32(x)
		case "blockdump":
			parameter.blockDump = (value == "yes")
		case "passphrase":
			parameter.passphrase = value
		}
	}

	fmt.Println()
	return nil
}

/*------------------------------------------------------------------------
 * int command_help(command_t *command, ttp_session_t *session);
 *
 * Offers help on either the list of available commands or a particular
 * command.  Returns 0 on success and nonzero on failure, which is not
 * possible, but it normalizes the API.
 *------------------------------------------------------------------------*/
func CommandHelp(args []string) {
	/* if no command was supplied */
	if len(args) < 1 {
		fmt.Println("Help is available for the following commands:\n")
		fmt.Println("    close    connect    get    dir    help    quit    set\n")
		fmt.Println("Use 'help <command>' for help on an individual command.\n")

		/* handle the CLOSE command */
	} else if args[0] == "close" {
		fmt.Println("Usage: close\n")
		fmt.Println("Closes the current connection to a remote Tsunami server.\n")

		/* handle the CONNECT command */
	} else if args[0] == "connect" {
		fmt.Println("Usage: connect")
		fmt.Println("       connect <remote-host>")
		fmt.Println("       connect <remote-host> <remote-port>\n")
		fmt.Println("Opens a connection to a remote Tsunami server.  If the host and port")
		fmt.Println("are not specified, default values are used.  (Use the 'set' command to")
		fmt.Println("modify these values.)\n")
		fmt.Println("After connecting, you will be prompted to enter a shared secret for")
		fmt.Println("authentication.\n")

		/* handle the GET command */
	} else if args[0] == "get" {
		fmt.Println("Usage: get <remote-file>")
		fmt.Println("       get <remote-file> <local-file>\n")
		fmt.Println("Attempts to retrieve the remote file with the given name using the")
		fmt.Println("Tsunami file transfer protocol.  If the local filename is not")
		fmt.Println("specified, the final part of the remote filename (after the last path")
		fmt.Println("separator) will be used.\n")

		/* handle the DIR command */
	} else if args[0] == "dir" {
		fmt.Println("Usage: dir\n")
		fmt.Println("Attempts to list the available remote files.\n")

		/* handle the HELP command */
	} else if args[0] == "help" {
		fmt.Println("Come on.  You know what that command does.\n")

		/* handle the QUIT command */
	} else if args[0] == "quit" {
		fmt.Println("Usage: quit\n")
		fmt.Println("Closes any open connection to a remote Tsunami server and exits the")
		fmt.Println("Tsunami client.\n")

		/* handle the SET command */
	} else if args[0] == "set" {
		fmt.Println("Usage: set")
		fmt.Println("       set <field>")
		fmt.Println("       set <field> <value>\n")
		fmt.Println("Sets one of the defaults to the given value.  If the value is omitted,")
		fmt.Println("the current value of the field is returned.  If the field is also")
		fmt.Println("omitted, the current values of all defaults are returned.\n")

		/* apologize for our ignorance */
	} else {
		fmt.Println("'%s' is not a recognized command.", args[0])
		fmt.Println("Use 'help' for a list of commands.\n")
	}
}

func CommandQuit(session *Session) {
	if session != nil && session.connection != nil {
		session.connection.Close()
	}

	fmt.Println("Thank you for using Tsunami Go version.")

	os.Exit(1)
	return
}
