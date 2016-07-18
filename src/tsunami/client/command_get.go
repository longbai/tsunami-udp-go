package client

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"time"

	"tsunami"
)

/*------------------------------------------------------------------------
 * int Command_get(Command_t *Command, ttp_session_t *session)
 *
 * Tries to initiate a file transfer for the remote file given in the
 * command.  If the user did not supply a local filename, we derive it
 * from the remote filename.  Returns 0 on a successful transfer and
 * nonzero on an error condition.
 *------------------------------------------------------------------------*/
func CommandGet(remotePath string, localPath string, session *Session) error {
	if remotePath == "" {
		return errors.New("Invalid command syntax (use 'help get' for details)")
	}
	if session == nil || session.connection == nil {
		return errors.New("Not connected to a Tsunami server")
	}

	// var datagram []byte       /* the buffer (in ring) for incoming blocks       */
	// var local_datagram []byte /* the local temp space for incoming block        */
	// var this_block uint32     /* the block number for the block just received   */
	// var this_type uint16      /* the block type for the block just received     */
	// var delta uint64          /* generic holder of elapsed times                */
	// var block uint32          /* generic holder of a block number               */

	xfer := &transfer{}
	session.tr = xfer

	// rexmit := &(xfer.retransmit)

	var f_total int = 1
	// var f_arrsize uint64 = 0
	multimode := false

	// var wait_u_sec int64 = 1
	var file_names []string
	var err error
	if remotePath == "*" {
		multimode = true
		file_names, err = session.multiFileRequest()
		if err != nil {
			return err
		}
		f_total = len(file_names)
	}

	/*---loop for single and multi file request---*/
	for i := 0; i < int(f_total); i++ {
		if multimode {
			xfer.remoteFileName = file_names[i]
			/* don't trim, GET* writes into remotefilename dir if exists,
			   otherwise into CWD */
			xfer.localFileName = file_names[i]
			fmt.Println("GET *: now requesting file ", xfer.localFileName)
		} else {
			xfer.remoteFileName = remotePath
			if localPath != "" {
				xfer.localFileName = localPath
			} else {
				xfer.localFileName = path.Base(remotePath)
			}
		}
		err = session.getSingleFile()
		if err != nil {
			break
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr,
			"Transfer not successful.  (WARNING: You may need to reconnect.)")
	}
	return err
}

func (session *Session) multiFileRequest() ([]string, error) {
	fmt.Println("Requesting all available files")
	/* Send request and try to calculate the RTT from client to server */
	t1 := time.Now()
	_, err := session.connection.Write([]byte("*\n"))
	if err != nil {
		return nil, err
	}
	b10 := make([]byte, 10)
	l, err := session.connection.Read(b10)
	if err != nil {
		return nil, err
	}
	// filearray_size := string(b10[:l])
	t2 := time.Now()
	fmt.Println("elapsed", t1, t2)
	l, err = session.connection.Read(b10)
	if err != nil {
		return nil, err
	}
	file_count := string(b10[:l])
	_, err = session.connection.Write([]byte("got size"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not request file")
		return nil, err
	}

	/* See if the request was successful */
	if l < 1 {
		err = errors.New("Could not read response to file request")
		fmt.Fprintln(os.Stderr, err)
		return nil, err
	}

	/* Calculate and convert RTT to u_sec, with +10% margin */
	// d := t2.Sub(t1).Nanoseconds()
	// wait_u_sec := (d + d/10) / 1000

	// f_arrsize, _ := strconv.ParseUint(filearray_size, 10, 64)
	f_total, _ := strconv.ParseUint(file_count, 10, 64)
	if f_total <= 0 {
		/* get the \x008 failure signal */
		dummy := make([]byte, 1)
		session.connection.Read(dummy)
		err = errors.New("Server advertised no files to get")
		fmt.Fprintln(os.Stderr, err)
		return nil, err
	}
	fmt.Printf("\nServer is sharing %v files\n", f_total)

	/* Read the file list */
	file_names := make([]string, f_total)

	fmt.Printf("Multi-GET of %v files:\n", f_total)
	for i := 0; i < int(f_total); i++ {
		tmpname, err := tsunami.ReadLine(session.connection, 1024)
		if err != nil {
			return nil, err
		}
		file_names[i] = tmpname
		fmt.Print(tmpname)
	}
	session.connection.Write([]byte("got list"))
	fmt.Println("")
	return file_names, nil
}

func (session *Session) getSingleFile() error {

	dumpcount := 0
	xfer := session.tr
	rexmit := &(xfer.retransmit)
	/* negotiate the file request with the server */
	if err := session.openTransfer(xfer.remoteFileName, xfer.localFileName); err != nil {
		fmt.Fprintln(os.Stderr, "File transfer request failed", err)
		return err
	}
	if err := session.openPort(); err != nil {
		fmt.Fprintln(os.Stderr, "Creation of data socket failed")
		return err
	}
	defer xfer.udpConnection.Close()

	rexmit.table = make([]uint32, DEFAULT_TABLE_SIZE)
	xfer.received = make([]byte, xfer.blockCount/8+2)

	xfer.ringBuffer = session.NewRingBuffer()

	ch := make(chan int)
	/* start up the disk I/O thread */
	go session.disk_thread(ch)

	/* Finish initializing the retransmission object */
	rexmit.tableSize = DEFAULT_TABLE_SIZE
	rexmit.indexMax = 0

	/* we start by expecting block #1 */
	xfer.nextBlock = 1
	xfer.gaplessToBlock = 0

	/*---------------------------
	 * START TIMING
	 *---------------------------*/

	xfer.stats = statistics{}
	xfer.stats.startUdpErrors = tsunami.Get_udp_in_errors()
	xfer.stats.thisUdpErrors = xfer.stats.startUdpErrors
	xfer.stats.startTime = time.Now()
	xfer.stats.thisTime = xfer.stats.startTime
	session.XsriptDataStart(xfer.stats.startTime)

	/* until we break out of the transfer */
	local_datagram := make([]byte, 6+session.param.blockSize)

	for {
		/* try to receive a datagram */
		_, err := xfer.udpConnection.Read(local_datagram)
		if err != nil {
			fmt.Fprintln(os.Stderr, "UDP data transmission error")
			fmt.Println("Apparently frozen transfer, trying to do retransmit request")
			if err = session.repeatRetransmit(); err != nil { /* repeat our requests */
				fmt.Fprintln(os.Stderr, "Repeat of retransmission requests failed")
				return err
			}

		}

		/* retrieve the block number and block type */
		this_block := binary.BigEndian.Uint32(local_datagram[:4]) // in range of 1..xfer.block_count
		this_type := binary.BigEndian.Uint16(local_datagram[4:6]) // TS_BLOCK_ORIGINAL etc

		/* keep statistics on received blocks */
		xfer.stats.totalBlocks++
		if this_type != tsunami.TS_BLOCK_RETRANSMISSION {
			xfer.stats.thisFlowOriginals++
		} else {
			xfer.stats.thisFlowRetransmitteds++
			xfer.stats.totalRecvdRetransmits++
		}

		/* main transfer control logic */
		if !xfer.ringBuffer.isFull() { /* don't let disk-I/O freeze stop feedback of stats to server */
			if session.gotBlock(this_block) != 0 ||
				this_type == tsunami.TS_BLOCK_TERMINATE || xfer.restartPending {
				/* insert new blocks into disk write ringbuffer */
				if session.gotBlock(this_block) != 0 {
					/* reserve ring space, copy the data in, confirm the reservation */
					datagram, err := xfer.ringBuffer.reserve()
					if err != nil {
						fmt.Fprintln(os.Stderr, "Error in accepting block reserve")
						return err
					}
					copy(datagram, local_datagram)
					if err = xfer.ringBuffer.confirm(); err != nil {
						fmt.Fprintln(os.Stderr, "Error in accepting block")
						return err
					}

					/* mark the block as received */
					xfer.received[this_block/8] |= (1 << (this_block % 8))
					if xfer.blocksLeft > 0 {
						xfer.blocksLeft--
					} else {
						fmt.Printf("Oops! Negative-going blocksLeft count at block: type=%c this=%u final=%u left=%u\n",
							this_type, this_block, xfer.blockCount, xfer.blocksLeft)
					}
				}

				/* transmit restart: avoid re-triggering on blocks still down the wire before server reacts */
				if (xfer.restartPending) && (this_type != tsunami.TS_BLOCK_TERMINATE) {
					if (this_block > xfer.restartLastIndex) && (this_block <= xfer.restartWireClearIndex) {
						goto send_stats
					}
				}

				/* queue any retransmits we need */
				if this_block > xfer.nextBlock {

					/* lossy transfer mode */
					if !session.param.lossless {
						if session.param.losswindow_ms == 0 {
							/* lossy transfer, no retransmits */
							xfer.gaplessToBlock = this_block
						} else {
							/* semi-lossy transfer, purge data past specified approximate time window */
							var path_capability float64
							path_capability =
								0.8 * (xfer.stats.thisTransmitRate + xfer.stats.thisRetransmitRate) // reduced effective Mbit/s rate
							path_capability *= 0.001 * float64(session.param.losswindow_ms) // MBit inside window, round-trip user estimated in losswindow_ms!

							min := 1024 * 1024 * uint32(path_capability) / (8 * session.param.blockSize) // # of blocks inside window
							if min > this_block-xfer.gaplessToBlock {
								min = this_block - xfer.gaplessToBlock
							}
							earliest_block := this_block - min

							if err = session.requestRetransmitBlocks(earliest_block, this_block); err != nil {
								return err
							}
							// hop over the missing section
							xfer.nextBlock = earliest_block
							xfer.gaplessToBlock = earliest_block
						}

						/* lossless transfer mode, request all missing data to be resent */
					} else {
						if err = session.requestRetransmitBlocks(xfer.nextBlock, this_block); err != nil {
							return err
						}
					}
				} //if(missing blocks)

				/* advance the index of the gapless section going from start block to highest block  */
				for session.gotBlock(xfer.gaplessToBlock+1) != 0 &&
					xfer.gaplessToBlock < xfer.blockCount {
					xfer.gaplessToBlock++
				}

				/* if this is an orignal, we expect to receive the successor to this block next */
				/* transmit restart note: these resent blocks are labeled original as well      */
				if this_type == tsunami.TS_BLOCK_ORIGINAL {
					xfer.nextBlock = this_block + 1
				}

				/* transmit restart: already got out of the missing blocks range? */
				if xfer.restartPending &&
					(xfer.nextBlock >= xfer.restartLastIndex) {
					xfer.restartPending = false
				}

				/* are we at the end of the transmission? */
				if this_type == tsunami.TS_BLOCK_TERMINATE {

					// fmt.Fprintf(os.Stderr, "Got end block: blk %u, final blk %u, left blks %u, tail %u, head %u\n",
					// 	this_block, xfer.blockCount, xfer.blocksLeft, xfer.gaplessToBlock, xfer.nextBlock)

					/* got all blocks by now */
					if xfer.blocksLeft == 0 {
						break
					} else if !session.param.lossless {
						if rexmit.indexMax == 0 && !xfer.restartPending {
							break
						}
					}

					/* add possible still missing blocks to retransmit list */
					if err = session.requestRetransmitBlocks(xfer.gaplessToBlock+1, xfer.blockCount); err != nil {
						return err
					}

					/* send the retransmit request list again */
					session.repeatRetransmit()
				}
			} //if(not a duplicate block)
		send_stats:
			/* repeat our server feedback and requests if it's time */
			if xfer.stats.totalBlocks%50 == 0 {

				/* if it's been at least 350ms */
				if tsunami.Get_usec_since(xfer.stats.thisTime) > tsunami.UPDATE_PERIOD {

					/* repeat our retransmission requests */
					if err = session.repeatRetransmit(); err != nil {
						fmt.Fprintln(os.Stderr, "Repeat of retransmission requests failed")
						return err
					}

					/* send and show our current statistics */
					session.updateStats()

					/* progress blockmap (DEBUG) */

					if session.param.blockDump {

						postfix := fmt.Sprintf(".bmap%d", dumpcount)
						dumpcount++
						xfer.dumpBlockmap(postfix)
					}
				}
			}
		} /* Transfer of the file completes here*/
		fmt.Println("Transfer complete. Flushing to disk and signaling server to stop...")
		/*---------------------------
		 * STOP TIMING
		 *---------------------------*/

		/* tell the server to quit transmitting */
		if err = session.requestStop(); err != nil {
			fmt.Fprintln(os.Stderr, "Could not request end of transfer")
			return err
		}

		/* add a stop block to the ring buffer */
		datagram, err := xfer.ringBuffer.reserve()
		//disk read block index 0 stop
		if err == nil {
			datagram[0] = 0
			datagram[1] = 0
			datagram[2] = 0
			datagram[3] = 0
		}
		if err = xfer.ringBuffer.confirm(); err != nil {
			fmt.Fprintln(os.Stderr, "Error in terminating disk thread", err)
		}

		/* wait for the disk thread to die */
		flag := <-ch
		if flag == -2 {
			fmt.Fprintln(os.Stderr, "Disk thread terminated with error")
		}

		/*------------------------------------
		 * MORE TRUE POINT TO STOP TIMING ;-)
		 *-----------------------------------*/
		// time here would contain the additional delays from the
		// local disk flush and server xfer shutdown - include or omit?

		/* get finishing time */
		xfer.stats.stopTime = time.Now()
		delta := tsunami.Get_usec_since(xfer.stats.startTime)

		/* count the truly lost blocks from the 'received' bitmap table */
		xfer.stats.totalLost = 0
		var block uint32
		for block = 1; block <= xfer.blockCount; block++ {
			if session.gotBlock(block) == 0 {
				xfer.stats.totalLost++
			}
		}

		session.displayResult(delta)

		session.XsriptDataStop(xfer.stats.stopTime)
		session.XsriptClose(uint64(delta))

		/* dump the received packet bitfield to a file, with added filename prefix ".blockmap" */
		if session.param.blockDump {
			xfer.dumpBlockmap(".blockmap")
		}

		/* close our open files */
		if xfer.localFile != nil {
			xfer.localFile.Close()
			xfer.localFile = nil
		}

		/* deallocate memory */
		// xfer.ringBuffer = nil
		// if rexmit.table != nil {
		// 	rexmit.table = nil
		// }
		// if xfer.received != nil {
		// 	xfer.received = nil
		// }

		/* update the target rate */
		if session.param.rateAdjust {
			session.param.targetRate = uint32(1.15 * 1e6 * (8.0 * int64(xfer.fileSize) / delta * 1e6))
			fmt.Printf("Adjusting target rate to %d Mbps for next transfer.\n",
				(int)(session.param.targetRate/1e6))
		}

	}
	return nil
}

/* display the final results */
func (session *Session) displayResult(delta int64) {
	xfer := session.tr
	mbit_thru := 8.0 * xfer.stats.totalBlocks * session.param.blockSize
	mbit_good := mbit_thru - 8.0*xfer.stats.totalRecvdRetransmits*session.param.blockSize
	mbit_file := 8.0 * xfer.fileSize
	mbit_thru /= (1024.0 * 1024.0)
	mbit_good /= (1024.0 * 1024.0)
	mbit_file /= (1024.0 * 1024.0)
	time_secs := delta / 1e6
	fmt.Printf("PC performance figure : %v packets dropped (if high this indicates receiving PC overload)\n",
		int64(xfer.stats.thisUdpErrors-xfer.stats.startUdpErrors))
	fmt.Printf("Transfer duration     : %0.2f seconds\n", time_secs)
	fmt.Printf("Total packet data     : %0.2f Mbit\n", mbit_thru)
	fmt.Printf("Goodput data          : %0.2f Mbit\n", mbit_good)
	fmt.Printf("File data             : %0.2f Mbit\n", mbit_file)
	fmt.Printf("Throughput            : %0.2f Mbps\n", int64(mbit_thru)/time_secs)
	fmt.Printf("Goodput w/ restarts   : %0.2f Mbps\n", int64(mbit_good)/time_secs)
	fmt.Printf("Final file rate       : %0.2f Mbps\n", int64(mbit_file)/time_secs)
	fmt.Printf("Transfer mode         : ")
	if session.param.lossless {
		if xfer.stats.totalLost == 0 {
			fmt.Println("lossless")
		} else {
			fmt.Printf("lossless mode - but lost count=%u > 0, please file a bug report!!\n", xfer.stats.totalLost)
		}
	} else {
		if session.param.losswindow_ms == 0 {
			fmt.Println("lossy")
		} else {
			fmt.Printf("semi-lossy, time window %d ms\n", session.param.losswindow_ms)
		}
		fmt.Printf("Data blocks lost      : %llu (%.2f%% of data) per user-specified time window constraint\n",
			xfer.stats.totalLost, (100.0*xfer.stats.totalLost)/xfer.blockCount)
	}
	fmt.Println("")
}

func (s *Session) requestRetransmitBlocks(start, end uint32) error {
	for block := start; block < end; block++ {
		if err := s.requestRetransmit(block); err != nil {
			fmt.Fprintln(os.Stderr, "Retransmission request failed")
			return err
		}
	}
	return nil
}

/*------------------------------------------------------------------------
 * void *disk_thread(void *arg);
 *
 * This is the thread that takes care of saved received blocks to disk.
 * It runs until the network thread sends it a datagram with a block
 * number of 0.  The return value has no meaning.
 *------------------------------------------------------------------------*/
func (session *Session) disk_thread(ch chan int) {
	/* while the world is turning */
	for {
		/* get another block */
		datagram := session.tr.ringBuffer.peek()
		blockIndex := binary.BigEndian.Uint32(datagram[:4])
		// blockType := binary.BigEndian.Uint16(datagram[4:6])

		/* quit if we got the mythical 0 block */
		if blockIndex == 0 {
			fmt.Println("!!!!")
			ch <- -1
			return
		}

		/* save it to disk */
		err := session.accept_block(blockIndex, datagram[6:])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Block accept failed")
			ch <- -2
			return
		}

		/* pop the block */
		session.tr.ringBuffer.pop()
	}
}

/*------------------------------------------------------------------------
 * int got_block(session_t* session, u_int32_t blocknr)
 *
 * Returns non-0 if the block has already been received
 *------------------------------------------------------------------------*/
func (s *Session) gotBlock(blocknr uint32) int {
	if blocknr > s.tr.blockCount {
		return 1
	}
	return int(s.tr.received[blocknr/8] & (1 << (blocknr % 8)))
}

/*------------------------------------------------------------------------
 * void dumpBlockmap(const char *postfix, const ttp_transfer_t *xfer)
 *
 * Writes the current bitmap of received block accounting into
 * a file named like the transferred file but with an extra postfix.
 *------------------------------------------------------------------------*/
func (xfer *transfer) dumpBlockmap(postfix string) {
	fname := xfer.localFileName + postfix
	fbits, err := os.OpenFile(fname, os.O_WRONLY, os.ModePerm)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not create a file for the blockmap dump")
		return
	}

	/* write: [4 bytes block_count] [map byte 0] [map byte 1] ... [map N (partial final byte)] */
	binary.Write(fbits, binary.LittleEndian, xfer.blockCount)
	fbits.Write(xfer.received[:xfer.blockCount/8+1])
	fbits.Close()
}
