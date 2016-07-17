package client

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	// "io"
	"net"
	"os"
	"time"

	"tsunami"
)

const MAX_RETRANSMISSION_BUFFER = 2048

const MAX_BLOCKS_QUEUED = 4096

/* statistical data */
type statistics struct {
	startTime time.Time /* when we started timing the transfer         */
	stopTime  time.Time /* when we finished timing the transfer        */
	thisTime  time.Time /* when we began this data collection period   */

	thisBlocks      uint32 /* the number of blocks in this interval       */
	thisRetransmits uint32 /* the number of retransmits in this interval  */

	totalBlocks           uint32 /* the total number of blocks transmitted      */
	totalRetransmits      uint32 /* the total number of retransmission requests */
	totalRecvdRetransmits uint32 /* the total number of received retransmits    */
	totalLost             uint32 /* the final number of data blocks lost        */

	thisFlowOriginals      uint32  /* the number of original blocks this interval */
	thisFlowRetransmitteds uint32  /* the number of re-tx'ed blocks this interval */
	thisTransmitRate       float64 /* the unfiltered transmission rate (bps)      */

	transmitRate       float64 /* the smoothed transmission rate (bps)        */
	thisRetransmitRate float64 /* the unfiltered retransmission rate (bps)    */
	errorRate          float64 /* the smoothed error rate (% x 1000)          */

	startUdpErrors int64 /* the initial UDP error counter value of OS   */
	thisUdpErrors  int64 /* the current UDP error counter value of OS   */
}

/* state of the retransmission table for a transfer */
type retransmit struct {
	table     []uint32 /* the table of retransmission blocks          */
	tableSize uint32   /* the size of the retransmission table        */
	indexMax  uint32   /* the maximum table index in active use       */
}

/* state of a TTP transfer */
type transfer struct {
	epoch                 uint32       /* the Unix epoch used to identify this run    */
	remoteFileName        string       /* the path to the file (on the server)        */
	localFileName         string       /* the path to the file (locally)              */
	localFile             *os.File     /* the open file that we're receiving          */
	transcript            *os.File     /* the transcript file that we're writing to   */
	udpConnection         *net.UDPConn /* the file descriptor of our UDP socket       */
	fileSize              uint64       /* the total file size (in bytes)              */
	blockCount            uint32       /* the total number of blocks in the file      */
	nextBlock             uint32       /* the index of the next block we expect       */
	gaplessToBlock        uint32       /* the last block in the fully received range  */
	retransmit            retransmit   /* the retransmission data for the transfer    */
	stats                 statistics   /* the statistical data for the transfer       */
	ringBuffer            *ringBuffer  /* the blocks waiting for a disk write         */
	received              []byte       /* bitfield for the received blocks of data    */
	blocksLeft            uint32       /* the number of blocks left to receive        */
	restartPending        bool         /* 1 to ignore too new packets                 */
	restartLastIndex      uint32       /* the last index in the restart list          */
	restartWireClearIndex uint32       /* the max on-wire block number before react   */
	onWireEstimate        uint32       /* the max packets on wire if RTT is 500ms     */
}

/* state of a Tsunami session as a whole */
type Session struct {
	param      Parameter    /* the TTP protocol parameters                 */
	tr         *transfer    /* the current transfer in progress, if any    */
	connection *net.TCPConn /* the connection to the remote server         */
	address    net.IP       /* the socket address of the remote server     */
}

/*------------------------------------------------------------------------
 * int authenticate(session_t *session, u_char *secret);
 *
 * Given an active Tsunami session, returns 0 if we are able to
 * negotiate authentication successfully and a non-zero value
 * otherwise.
 *
 * The negotiation process works like this:
 *
 *     (1) The server sends 512 bits of random data to the client
 *         [this process].
 *
 *     (2) The client XORs 512 bits of the shared secret onto this
 *         random data and responds with the MD5 hash of the result.
 *
 *     (3) The server does the same thing and compares the result.
 *         If the authentication succeeds, the server transmits a
 *         result byte of 0.  Otherwise, it transmits a non-zero
 *         result byte.
 *------------------------------------------------------------------------*/
func (s *Session) authenticate(secret string) error {
	random := make([]byte, 64) /* the buffer of random data               */
	result := make([]byte, 1)  /* the result byte from the server         */

	count, e := s.connection.Read(random)
	if e != nil {
		return e
	}
	if count < 64 {
		return errors.New("Could not read authentication challenge from server")
	}

	/* prepare the proof of the shared secret and destroy the password */
	digest := tsunami.PrepareProof(random, []byte(secret))

	/* send the response to the server */
	count, e = s.connection.Write(digest[:])
	if e != nil {
		return e
	}
	if count < 16 {
		return errors.New("Could not send authentication response")
	}

	count, e = s.connection.Read(result)
	if e != nil {
		return e
	}

	if count < 1 {
		return errors.New("Could not read authentication status")
	}

	if result[0] != 0 {
		return errors.New("Authentication failed")
	}
	return nil
}

/*------------------------------------------------------------------------
 * int negotiate(session_t *session);
 *
 * Performs all of the negotiation with the remote server that is done
 * prior to authentication.  At the moment, this consists of verifying
 * identical protocol revisions between the client and server.  Returns
 * 0 on success and non-zero on failure.
 *
 * Values are transmitted in network byte order.
 *------------------------------------------------------------------------*/
func (s *Session) negotiate() error {
	buf := make([]byte, 4)

	binary.BigEndian.PutUint32(buf, tsunami.PROTOCOL_REVISION)

	count, e := s.connection.Write(buf)
	if e != nil {
		return e
	}

	if count < 4 {
		return errors.New("Could not send protocol revision number")
	}

	count, e = s.connection.Read(buf)
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
 * int open_transfer(session_t *session,
 *                       const char *remoteFilename,
 *                       const char *localFilename);
 *
 * Tries to create a new TTP file request object for the given session
 * by submitting a file request to the server (which is waiting for
 * the name of a file to transfer).  If the request is accepted, we
 * retrieve the file parameters, open the file for writing, and return
 * 0 for success.  If anything goes wrong, we return a non-zero value.
 *------------------------------------------------------------------------*/
func (s *Session) openTransfer(remoteFilename, localFilename string) error {
	result := make([]byte, 1) /* the result byte from the server     */

	count, e := fmt.Fprintf(s.connection, "%v\n", remoteFilename)
	if e != nil {
		return e
	}

	count, e = s.connection.Read(result)
	if e != nil {
		return e
	}
	if count < 1 {
		return errors.New("Could not read response to file request")
	}

	if result[0] != 0 {
		return errors.New("Server: File does not exist or cannot be transmitted")
	}

	buf := bytes.NewBuffer(nil)

	binary.Write(buf, binary.BigEndian, s.param.blockSize)
	binary.Write(buf, binary.BigEndian, s.param.targetRate)
	binary.Write(buf, binary.BigEndian, s.param.errorRate)
	count, e = s.connection.Write(buf.Bytes())
	if e != nil {
		return e
	}
	buf.Reset()

	binary.Write(buf, binary.BigEndian, s.param.slowerNum)
	binary.Write(buf, binary.BigEndian, s.param.slowerDen)
	binary.Write(buf, binary.BigEndian, s.param.fasterNum)
	binary.Write(buf, binary.BigEndian, s.param.fasterDen)
	count, e = s.connection.Write(buf.Bytes())
	if e != nil {
		return e
	}

	s.tr.remoteFileName = remoteFilename
	s.tr.localFileName = localFilename

	t8 := make([]byte, 8)
	count, e = s.connection.Read(t8)
	if e != nil {
		return e
	}
	if count < 8 {
		return errors.New("Could not read file size")
	}
	s.tr.fileSize = binary.BigEndian.Uint64(t8)
	t4 := t8[:4]
	count, e = s.connection.Read(t4)
	if e != nil {
		return e
	}
	if count < 4 {
		return errors.New("Could not read block size")
	}
	blockSize := binary.BigEndian.Uint32(t4)
	if blockSize != s.param.blockSize {
		return errors.New("Block size disagreement")
	}

	count, e = s.connection.Read(t4)
	if e != nil {
		return e
	}
	if count < 4 {
		return errors.New("Could not read block count")
	}
	s.tr.blockCount = binary.BigEndian.Uint32(t4)

	count, e = s.connection.Read(t4)
	if e != nil {
		return e
	}
	if count < 4 {
		return errors.New("Could not read epoch")
	}
	s.tr.epoch = binary.BigEndian.Uint32(t4)

	/* we start out with every block yet to transfer */
	s.tr.blocksLeft = s.tr.blockCount

	if _, err := os.Stat(s.tr.localFileName); err == nil {
		fmt.Printf("Warning: overwriting existing file '%v'\n", s.tr.localFileName)
	}

	s.tr.localFile, e = os.OpenFile(s.tr.localFileName, os.O_WRONLY, os.ModePerm)
	if e != nil {
		return errors.New("Could not open local file for writing")
	}

	/* make crude estimate of blocks on the wire if RTT delay is 500ms */
	temp := 0.5 * float64(s.param.targetRate) / (8.0 * float64(s.param.blockSize))
	s.tr.onWireEstimate = uint32(temp)
	if s.tr.blockCount < s.tr.onWireEstimate {
		s.tr.onWireEstimate = s.tr.blockCount
	}
	s.XsriptOpen()

	return nil
}

/*------------------------------------------------------------------------
 * int open_port(session_t *session);
 *
 * Creates a new UDP socket for receiving the file data associated with
 * our pending transfer and communicates the port number back to the
 * server.  Returns 0 on success and non-zero on failure.
 *------------------------------------------------------------------------*/
func (s *Session) openPort() error {
	a := fmt.Sprintf(":%v", s.param.clientPort)
	addr, err := net.ResolveUDPAddr("udp", a)
	if err != nil {
		return err
	}
	s.tr.udpConnection, err = net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}
	p := make([]byte, 2)
	binary.BigEndian.PutUint16(p, uint16(addr.Port))
	_, err = s.connection.Write(p)
	if err != nil {
		s.tr.udpConnection.Close()
		return err
	}

	return nil
}

/*------------------------------------------------------------------------
 * int repeat_retransmit(session_t *session);
 *
 * Tries to repeat all of the outstanding retransmit requests for the
 * current transfer on the given session.  Returns 0 on success and
 * non-zero on error.  This also takes care of maintanence operations
 * on the transmission table, such as relocating the entries toward the
 * bottom of the array.
 *------------------------------------------------------------------------*/
func (s *Session) repeatRetransmit() error {
	var retransmission [MAX_RETRANSMISSION_BUFFER]tsunami.Retransmission

	s.tr.stats.thisRetransmits = 0
	var count uint32
	var entry uint32
	var block uint32
	transmit := &s.tr.retransmit
	// fmt.Fprintln(os.Stderr, "ttp_repeat_retransmit: index_max=", transmit.indexMax)

	/* discard received blocks from the list and prepare retransmit requests */
	for entry = 0; (entry < transmit.indexMax) && (count < MAX_RETRANSMISSION_BUFFER); entry++ {
		/* get the block number */
		block = transmit.table[entry]
		/* if we want the block */
		if block != 0 && s.gotBlock(block) == 0 {
			/* save it */
			transmit.table[count] = block
			/* insert retransmit request */
			retransmission[count].RequestType = tsunami.REQUEST_RETRANSMIT
			retransmission[count].Block = block
			count++
		}
	}

	/* if there are too many entries, restart transfer from earlier point */
	if count >= MAX_RETRANSMISSION_BUFFER {
		/* restart from first missing block */
		block = s.tr.blockCount
		if s.tr.blockCount > s.tr.gaplessToBlock+1 {
			block = s.tr.gaplessToBlock + 1
		}
		retransmission[0].RequestType = tsunami.REQUEST_RESTART
		retransmission[0].Block = block

		_, err := s.connection.Write(tsunami.Retransmissions(retransmission[:1]).Bytes())
		if err != nil {
			fmt.Fprintln(os.Stderr, "Could not send restart-at request")
			return err
		}

		/* remember the request so we can then ignore blocks that are still on the wire */
		s.tr.restartPending = true
		s.tr.restartLastIndex = transmit.table[transmit.indexMax-1]
		s.tr.restartWireClearIndex = s.tr.blockCount
		if s.tr.blockCount > s.tr.restartLastIndex+s.tr.onWireEstimate {
			s.tr.restartWireClearIndex = s.tr.restartLastIndex + s.tr.onWireEstimate
		}

		/* reset the retransmission table and head block */
		transmit.indexMax = 0
		s.tr.nextBlock = block
		s.tr.stats.thisRetransmits = MAX_RETRANSMISSION_BUFFER

		/* queue is small enough */
	} else {

		/* update to shrunken size */
		transmit.indexMax = count

		/* update the statistics */
		s.tr.stats.thisRetransmits = count
		s.tr.stats.totalRetransmits += count

		/* send out the requests */
		if count > 0 {
			_, err := s.connection.Write(tsunami.Retransmissions(retransmission[:count]).Bytes())
			if err != nil {
				fmt.Fprintln(os.Stderr, "Could not send retransmit requests")
				return err
			}
		}

	}

	fmt.Fprintln(os.Stderr, "ttp_repeat_retransmit: post-index_max=", transmit.indexMax)
	return nil
}

/*------------------------------------------------------------------------
 * int request_retransmit(session_t *session, u_int32_t block);
 *
 * Requests a retransmission of the given block in the current transfer.
 * Returns 0 on success and non-zero otherwise.
 *------------------------------------------------------------------------*/
func (s *Session) requestRetransmit(block uint32) error {
	// u_int32_t     tmp32_ins = 0, tmp32_up;
	// u_int32_t     idx = 0;

	// u_int32_t    *ptr;
	rexmit := &(s.tr.retransmit)

	/* double checking: if we already got the block, don't add it */
	if s.gotBlock(block) != 0 {
		return nil
	}

	/* if we don't have space for the request */
	if rexmit.indexMax >= rexmit.tableSize {

		/* don't overgrow the table */
		if rexmit.indexMax >= 32*MAX_RETRANSMISSION_BUFFER {
			return nil
		}

		tableLarge := make([]uint32, len(rexmit.table)*2)
		copy(tableLarge, rexmit.table)
		rexmit.table = tableLarge
		rexmit.tableSize *= 2
		// fmt.Fprintf(os.Stderr, "ttp_request_retransmit: new table size is %v entries\n", rexmit.tableSize)
	}

	//ifndef sort
	/* store the request */
	rexmit.table[rexmit.indexMax] = block
	rexmit.indexMax++
	//else
	// s.RETX_REQBLOCK_SORTING(rexmit, block)
	//endif
	return nil
}

/*
 * Store the request via "insertion sort"
 * this maintains a sequentially sorted table and discards duplicate requests,
 * and does not flood the net with so many unnecessary retransmissions like old Tsunami did
 * -- however, this can be very slow on high loss transfers! with slow CPU this causes
 *    even more loss : consider well if you want to enable the feature or not
 */
func (s *Session) RETX_REQBLOCK_SORTING(rexmit *retransmit, block uint32) {
	/* seek to insertion point or end - could use binary search here... */
	var idx uint32
	for (idx < rexmit.indexMax) && (rexmit.table[idx] < block) {
		idx++
	}

	/* insert the entry */
	if idx == rexmit.indexMax {
		rexmit.table[rexmit.indexMax] = block
		rexmit.indexMax++
	} else if rexmit.table[idx] == block {
		// fprintf(stderr, "duplicate retransmit req for block %d discarded\n", block);
	} else {
		/* insert and shift remaining table upwards - linked list could be nice... */
		tmp32_ins := block
		var tmp32_up uint32

		tmp32_up = rexmit.table[idx]
		rexmit.table[idx] = tmp32_ins
		idx++
		tmp32_ins = tmp32_up
		for idx <= rexmit.indexMax {
			tmp32_up = rexmit.table[idx]
			rexmit.table[idx] = tmp32_ins
			idx++
			tmp32_ins = tmp32_up
		}

		rexmit.indexMax++
	}

}

/*------------------------------------------------------------------------
 * int request_stop(session_t *session);
 *
 * Requests that the server stop transmitting data for the current
 * file transfer in the given session.  This is done by sending a
 * retransmission request with a type of REQUEST_STOP.  Returns 0 on
 * success and non-zero otherwise.  Success means that we successfully
 * requested, not that we successfully halted.
 *------------------------------------------------------------------------*/
func (s *Session) requestStop() error {
	var retransmission []tsunami.Retransmission = []tsunami.Retransmission{tsunami.Retransmission{0, 0, 0}}
	retransmission[0].RequestType = tsunami.REQUEST_STOP

	/* send out the request */
	_, err := s.connection.Write(tsunami.Retransmissions(retransmission).Bytes())
	if err != nil {
		return err
	}
	return nil
}

const u_mega = int64(1024 * 1024)

const u_giga = int64(1024 * 1024 * 1024)

/*------------------------------------------------------------------------
 * int update_stats(session_t *session);
 *
 * This routine must be called every interval to update the statistics
 * for the progress of the ongoing file transfer.  Returns 0 on success
 * and non-zero on failure.  (There is not currently any way to fail.)
 *------------------------------------------------------------------------*/
func (s *Session) updateStats() {
	now_epoch := time.Now() /* the current Unix epoch                         */
	var delta int64         /* time delta since last statistics update (usec) */
	var delta_total int64   /* time delta since start of transmission (usec)  */
	var temp int64          /* temporary value for building the elapsed time  */

	var data_total float64       /* the total amount of data transferred (bytes)   */
	var data_this float64        /* the amount of data since last stat time        */
	var data_this_rexmit float64 /* the amount of data in received retransmissions */
	// var data_this_goodpt float64     /* the amount of data as non-lost packets         */
	var retransmits_fraction float64 /* how many retransmit requests there were vs received blocks */

	retransmission := make([]tsunami.Retransmission, 1)

	stats := &s.tr.stats

	/* find the total time elapsed */
	delta = tsunami.Get_usec_since(stats.thisTime)
	temp = tsunami.Get_usec_since(stats.startTime)
	milliseconds := (temp % 1000000) / 1000
	temp /= 1000000
	seconds := temp % 60
	temp /= 60
	minutes := temp % 60
	temp /= 60
	hours := temp

	d_seconds := delta / 1e6
	d_seconds_total := delta_total / 1e6

	/* find the amount of data transferred (bytes) */
	data_total = float64(s.param.blockSize) * float64(stats.totalBlocks)
	data_this = float64(s.param.blockSize) * float64(stats.totalBlocks-stats.thisBlocks)
	data_this_rexmit = float64(s.param.blockSize) * float64(stats.thisFlowRetransmitteds)
	// data_this_goodpt = float64(s.param.blockSize) * float64(stats.thisFlowOriginals)

	/* get the current UDP receive error count reported by the operating system */
	stats.thisUdpErrors = tsunami.Get_udp_in_errors()

	/* precalculate some fractions */
	retransmits_fraction = float64(stats.thisRetransmits) / (1.0 + float64(stats.thisRetransmits+stats.totalBlocks-stats.thisBlocks))
	ringfill_fraction := float64(s.tr.ringBuffer.countData) / MAX_BLOCKS_QUEUED
	total_retransmits_fraction := float64(stats.totalRetransmits) / float64(stats.totalRetransmits+stats.totalBlocks)

	/* update the rate statistics */
	// incoming transmit rate R = goodput R (Mbit/s) + retransmit R (Mbit/s)
	stats.thisTransmitRate = 8.0 * data_this / float64(d_seconds*u_mega)
	stats.thisRetransmitRate = 8.0 * data_this_rexmit / float64(d_seconds*u_mega)
	data_total_rate := 8.0 * data_total / float64(d_seconds_total*u_mega)

	fb := float64(s.param.history) / 100.0 // feedback
	ff := 1.0 - fb                         // feedforward

	// IIR filter rate R
	stats.transmitRate = fb*stats.transmitRate + ff*stats.thisRetransmitRate

	// IIR filtered composite error and loss, some sort of knee function
	stats.errorRate = fb*stats.errorRate + ff*500*100*(retransmits_fraction+ringfill_fraction)

	/* send the current error rate information to the server */
	retransmission[0].RequestType = tsunami.REQUEST_ERROR_RATE
	retransmission[0].ErrorRate = uint32(s.tr.stats.errorRate)
	_, err := s.connection.Write(tsunami.Retransmissions(retransmission).Bytes())
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not send error rate information", err)
		return
	}

	/* build the stats string */
	statsFlags := s.statsFlags()
	//matlab
	// format := "%02d\t%02d\t%02d\t%03d\t%4u\t%6.2f\t%6.1f\t%5.1f\t%7u\t%6.1f\t%6.1f\t%5.1f\t%5d\t%5d\t%7u\t%8u\t%8Lu\t%s\n"
	format := "%02d:%02d:%02d.%03d %4u %6.2fM %6.1fMbps %5.1f%% %7u %6.1fG %6.1fMbps %5.1f%% %5d %5d %7u %8u %8u %s\n"
	/* print to the transcript if the user wants */
	statusLine := fmt.Sprintf(format, hours, minutes, seconds, milliseconds,
		stats.totalBlocks-stats.thisBlocks,
		stats.thisRetransmitRate,
		stats.thisTransmitRate,
		100.0*retransmits_fraction,
		s.tr.stats.totalBlocks,
		data_total/float64(u_giga),
		data_total_rate,
		100.0*total_retransmits_fraction,
		s.tr.retransmit.indexMax,
		s.tr.ringBuffer.countData,
		s.tr.blocksLeft,
		stats.thisRetransmits,
		uint64(stats.thisUdpErrors-stats.startUdpErrors),
		statsFlags)

	/* give the user a show if they want it */
	if s.param.verbose {
		/* screen mode */
		if s.param.outputMode == SCREEN_MODE {
			fmt.Printf("\033[2J\033[H")
			fmt.Printf("Current time:   %s\n", now_epoch.Format(time.RFC3339))
			fmt.Printf("Elapsed time:   %02d:%02d:%02d.%03d\n\n", hours, minutes, seconds, milliseconds)
			fmt.Printf("Last interval\n--------------------------------------------------\n")
			fmt.Printf("Blocks count:     %u\n", stats.totalBlocks-stats.thisBlocks)
			fmt.Printf("Data transferred: %0.2f GB\n", data_this/float64(u_giga))
			fmt.Printf("Transfer rate:    %0.2f Mbps\n", stats.thisTransmitRate)
			fmt.Printf("Retransmissions:  %u (%0.2f%%)\n\n", stats.thisRetransmits, 100.0*retransmits_fraction)
			fmt.Printf("Cumulative\n--------------------------------------------------\n")
			fmt.Printf("Blocks count:     %u\n", s.tr.stats.totalBlocks)
			fmt.Printf("Data transferred: %0.2f GB\n", data_total/float64(u_giga))
			fmt.Printf("Transfer rate:    %0.2f Mbps\n", data_total_rate)
			fmt.Printf("Retransmissions:  %u (%0.2f%%)\n", stats.totalRetransmits, 100.0*total_retransmits_fraction)
			fmt.Printf("Flags          :  %s\n\n", statsFlags)
			fmt.Printf("OS UDP rx errors: %u\n", uint64(stats.thisUdpErrors-stats.startUdpErrors))

			/* line mode */
		} else {
			// s.iteration++
			// if s.iteration%23 == 0 {
			// 	fmt.Printf("             last_interval                   transfer_total                   buffers      transfer_remaining  OS UDP\n")
			// 	fmt.Printf("time          blk    data       rate rexmit     blk    data       rate rexmit queue  ring     blk   rt_len      err \n")
			// }
			fmt.Printf("%s", statusLine)
		}
		os.Stdout.Sync()
	}

	s.XsriptDataLog(statusLine)

	/* reset the statistics for the next interval */
	stats.thisBlocks = stats.totalBlocks
	stats.thisRetransmits = 0
	stats.thisFlowOriginals = 0
	stats.thisFlowRetransmitteds = 0
	stats.thisTime = time.Now()
}

func (s *Session) statsFlags() string {
	f1 := '-'
	if s.tr.restartPending {
		f1 = 'R'
	}
	f2 := 'F'
	if s.tr.ringBuffer.spaceReady {
		f2 = '-'
	}
	return fmt.Sprintf("%c%c", f1, f2)
}
