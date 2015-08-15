package client

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	// "sync"
	"time"

	"tsunami"
)

const PROTOCOL_REVISION = 0x20061025
const MAX_RETRANSMISSION_BUFFER = 2048

const MAX_BLOCKS_QUEUED = 4096
const REQUEST_RETRANSMIT = 0
const REQUEST_RESTART = 1
const REQUEST_STOP = 2
const REQUEST_ERROR_RATE = 3

/* statistical data */
// typedef struct {
//     struct timeval      start_time;               /* when we started timing the transfer         */
//     struct timeval      stop_time;                /* when we finished timing the transfer        */
//     struct timeval      this_time;                /* when we began this data collection period   */
//     u_int32_t           this_blocks;              /* the number of blocks in this interval       */
//     u_int32_t           this_retransmits;         /* the number of retransmits in this interval  */
//     u_int32_t           total_blocks;             /* the total number of blocks transmitted      */
//     u_int32_t           total_retransmits;        /* the total number of retransmission requests */
//     u_int32_t           total_recvd_retransmits;  /* the total number of received retransmits    */
//     u_int32_t           total_lost;               /* the final number of data blocks lost        */
//     u_int32_t           this_flow_originals;      /* the number of original blocks this interval */
//     u_int32_t           this_flow_retransmitteds; /* the number of re-tx'ed blocks this interval */
//     double              this_transmit_rate;       /* the unfiltered transmission rate (bps)      */
//     double              transmit_rate;            /* the smoothed transmission rate (bps)        */
//     double              this_retransmit_rate;     /* the unfiltered retransmission rate (bps)    */
//     double              error_rate;               /* the smoothed error rate (% x 1000)          */
//     u_int64_t           start_udp_errors;         /* the initial UDP error counter value of OS   */
//     u_int64_t           this_udp_errors;          /* the current UDP error counter value of OS   */
// } statistics_t;

type statistics struct {
	startTime time.Time
	stopTime  time.Time
	thisTime  time.Time

	thisBlocks      uint32
	thisRetransmits uint32

	totalBlocks           uint32
	totalRetransmits      uint32
	totalRecvdRetransmits uint32
	totalLost             uint32

	thisFlowOriginals      uint32
	thisFlowRetransmitteds uint32

	thisTransmitRate   float64
	transmitRate       float64
	thisRetransmitRate float64
	errorRate          float64

	startUdpErrors int64
	thisUdpErrors  int64
}

/* state of the retransmission table for a transfer */
// typedef struct {
//     u_int32_t          *table;                    /* the table of retransmission blocks          */
//     u_int32_t           table_size;               /* the size of the retransmission table        */
//     u_int32_t           index_max;                /* the maximum table index in active use       */
// } retransmit_t;

type retransmit struct {
	table     []uint32
	tableSize uint32
	indexMax  uint32
}

/* state of a TTP transfer */
// typedef struct {
//     time_t              epoch;                    /* the Unix epoch used to identify this run    */
//     const char         *remote_filename;          /* the path to the file (on the server)        */
//     const char         *local_filename;           /* the path to the file (locally)              */
//     FILE               *file;                     /* the open file that we're receiving          */
//     FILE               *vsib;                     /* the vsib file number                        */
//     FILE               *transcript;               /* the transcript file that we're writing to   */
//     int                 udp_fd;                   /* the file descriptor of our UDP socket       */
//     u_int64_t           file_size;                /* the total file size (in bytes)              */
//     u_int32_t           block_count;              /* the total number of blocks in the file      */
//     u_int32_t           next_block;               /* the index of the next block we expect       */
//     u_int32_t           gapless_to_block;         /* the last block in the fully received range  */
//     retransmit_t        retransmit;               /* the retransmission data for the transfer    */
//     statistics_t        stats;                    /* the statistical data for the transfer       */
//     ring_buffer_t      *ring_buffer;              /* the blocks waiting for a disk write         */
//     u_char             *received;                 /* bitfield for the received blocks of data    */
//     u_int32_t           blocks_left;              /* the number of blocks left to receive        */
//     u_char              restart_pending;          /* 1 to ignore too new packets                 */
//     u_int32_t           restart_lastidx;          /* the last index in the restart list          */
//     u_int32_t           restart_wireclearidx;     /* the max on-wire block number before react   */
//     u_int32_t           on_wire_estimate;         /* the max packets on wire if RTT is 500ms     */
// } ttp_transfer_t;

type transfer struct {
	epoch                 uint32
	remoteFileName        string
	localFileName         string
	localFile             io.Writer
	udpConnection         *net.UDPConn
	fileSize              uint64
	blockCount            uint32
	nextBlock             uint32
	gaplessToBlock        uint32
	retransmit            retransmit
	stats                 statistics
	ringBuffer            ring_buffer
	received              []byte
	blocksLeft            uint32
	restartPending        bool
	restartLastIndex      uint32
	restartWireClearIndex uint32
	onWireEstimate        uint32
}

type Session struct {
	param      Parameter
	tr         transfer
	address    net.IP
	connection net.Conn
}

/*------------------------------------------------------------------------
 * int ttp_authenticate(ttp_session_t *session, u_char *secret);
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
func (s *Session) ttp_authenticate(secret string) error {
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
 * int ttp_negotiate(ttp_session_t *session);
 *
 * Performs all of the negotiation with the remote server that is done
 * prior to authentication.  At the moment, this consists of verifying
 * identical protocol revisions between the client and server.  Returns
 * 0 on success and non-zero on failure.
 *
 * Values are transmitted in network byte order.
 *------------------------------------------------------------------------*/
func (s *Session) ttp_negotiate() error {
	buf := make([]byte, 4)

	binary.BigEndian.PutUint32(buf, PROTOCOL_REVISION)

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
	if x != PROTOCOL_REVISION {
		return errors.New("Protocol negotiation failed")
	}
	return nil
}

/*------------------------------------------------------------------------
 * int ttp_open_transfer(ttp_session_t *session,
 *                       const char *remote_filename,
 *                       const char *local_filename);
 *
 * Tries to create a new TTP file request object for the given session
 * by submitting a file request to the server (which is waiting for
 * the name of a file to transfer).  If the request is accepted, we
 * retrieve the file parameters, open the file for writing, and return
 * 0 for success.  If anything goes wrong, we return a non-zero value.
 *------------------------------------------------------------------------*/
func (s *Session) ttp_open_transfer(remote_filename, local_filename string) error {
	result := make([]byte, 1) /* the result byte from the server     */

	count, e := fmt.Fprintf(s.connection, "%v\n", remote_filename)
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

	s.tr.remoteFileName = remote_filename
	s.tr.localFileName = local_filename

	t8 := make([]byte, 8)
	count, e = s.connection.Read(t8)
	if e != nil {
		return e
	}
	if count < 8 {
		return errors.New("Could not read file size")
	}
	s.tr.fileSize = binary.BigEndian.Uint64(t8)
	t4 := make([]byte, 4)
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

	if s.param.transcript {
		//xscript_open(session);
	}

	return nil
}

/*------------------------------------------------------------------------
 * int ttp_open_port(ttp_session_t *session);
 *
 * Creates a new UDP socket for receiving the file data associated with
 * our pending transfer and communicates the port number back to the
 * server.  Returns 0 on success and non-zero on failure.
 *------------------------------------------------------------------------*/
func (s *Session) ttp_open_port() error {
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
 * int got_block(ttp_session_t* session, u_int32_t blocknr)
 *
 * Returns non-0 if the block has already been received
 *------------------------------------------------------------------------*/
func (s *Session) got_block(blocknr uint32) int {
	if blocknr > s.tr.blockCount {
		return 1
	}
	return int(s.tr.received[blocknr/8] & (1 << (blocknr % 8)))
}

/*------------------------------------------------------------------------
 * int ttp_repeat_retransmit(ttp_session_t *session);
 *
 * Tries to repeat all of the outstanding retransmit requests for the
 * current transfer on the given session.  Returns 0 on success and
 * non-zero on error.  This also takes care of maintanence operations
 * on the transmission table, such as relocating the entries toward the
 * bottom of the array.
 *------------------------------------------------------------------------*/
func (s *Session) ttp_repeat_retransmit() error {
	var retransmission [MAX_RETRANSMISSION_BUFFER]tsunami.Retransmission

	s.tr.stats.thisRetransmits = 0
	var count uint32
	var entry uint32
	var block uint32
	transmit := &s.tr.retransmit
	/* discard received blocks from the list and prepare retransmit requests */
	for entry = 0; (entry < transmit.indexMax) && (count < MAX_RETRANSMISSION_BUFFER); entry++ {

		/* get the block number */
		block = transmit.table[entry]

		/* if we want the block */
		if block != 0 && s.got_block(block) == 0 {

			/* save it */
			transmit.table[count] = block

			/* insert retransmit request */
			retransmission[count].RequestType = REQUEST_RETRANSMIT
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
		retransmission[0].RequestType = REQUEST_RESTART
		retransmission[0].Block = block

		_, err := s.connection.Write(tsunami.Retransmissions(retransmission[:1]).Bytes())
		if err != nil {
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
				return err
			}
		}

	} //if(num entries)

	return nil
}

/*------------------------------------------------------------------------
 * int ttp_request_retransmit(ttp_session_t *session, u_int32_t block);
 *
 * Requests a retransmission of the given block in the current transfer.
 * Returns 0 on success and non-zero otherwise.
 *------------------------------------------------------------------------*/
func (s *Session) ttp_request_retransmit(block uint32) error {
	// u_int32_t     tmp32_ins = 0, tmp32_up;
	// u_int32_t     idx = 0;

	// u_int32_t    *ptr;
	rexmit := &(s.tr.retransmit)

	/* double checking: if we already got the block, don't add it */
	if s.got_block(block) != 0 {
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
	}

	/*
	 * Store the request via "insertion sort"
	 * this maintains a sequentially sorted table and discards duplicate requests,
	 * and does not flood the net with so many unnecessary retransmissions like old Tsunami did
	 * -- however, this can be very slow on high loss transfers! with slow CPU this causes
	 *    even more loss : consider well if you want to enable the feature or not
	 */

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

	/* we succeeded */
	return nil
}

/*------------------------------------------------------------------------
 * int ttp_request_stop(ttp_session_t *session);
 *
 * Requests that the server stop transmitting data for the current
 * file transfer in the given session.  This is done by sending a
 * retransmission request with a type of REQUEST_STOP.  Returns 0 on
 * success and non-zero otherwise.  Success means that we successfully
 * requested, not that we successfully halted.
 *------------------------------------------------------------------------*/
func (s *Session) ttp_request_stop() error {
	var retransmission []tsunami.Retransmission = []tsunami.Retransmission{tsunami.Retransmission{0, 0, 0}}
	retransmission[0].RequestType = REQUEST_STOP

	/* send out the request */
	_, err := s.connection.Write(tsunami.Retransmissions(retransmission).Bytes())
	if err != nil {
		return err
	}
	return nil
}

/*------------------------------------------------------------------------
 * int ttp_update_stats(ttp_session_t *session);
 *
 * This routine must be called every interval to update the statistics
 * for the progress of the ongoing file transfer.  Returns 0 on success
 * and non-zero on failure.  (There is not currently any way to fail.)
 *------------------------------------------------------------------------*/
func (s *Session) ttp_update_stats() error {
	// time_t            now_epoch = time(NULL);                 /* the current Unix epoch                         */
	// u_int64_t         delta;                                  /* time delta since last statistics update (usec) */
	// double            d_seconds;
	// u_int64_t         delta_total;                            /* time delta since start of transmission (usec)  */
	// double            d_seconds_total;
	// u_int64_t         temp;                                   /* temporary value for building the elapsed time  */
	// int               hours, minutes, seconds, milliseconds;  /* parts of the elapsed time                      */
	// double            data_total;                             /* the total amount of data transferred (bytes)   */
	// double            data_total_rate;
	// double            data_this;                              /* the amount of data since last stat time        */
	// double            data_this_rexmit;                       /* the amount of data in received retransmissions */
	// double            data_this_goodpt;                       /* the amount of data as non-lost packets         */
	// double            retransmits_fraction;                   /* how many retransmit requests there were vs received blocks */
	// double            total_retransmits_fraction;
	// double            ringfill_fraction;
	// statistics_t     *stats = &(session->transfer.stats);
	retransmission := make([]tsunami.Retransmission, 1)
	// int               status;
	// static u_int32_t  iteration = 0;
	// static char       stats_line[128];
	// static char       stats_flags[8];

	stats := &s.tr.stats

	// double ff, fb;

	u_mega := int64(1024 * 1024)
	// u_giga := 1024 * 1024 * 1024

	/* find the total time elapsed */
	delta := tsunami.Get_usec_since(stats.thisTime)
	temp := tsunami.Get_usec_since(stats.startTime)
	// delta_total := temp
	// milliseconds := (temp % 1000000) / 1000
	temp /= 1000000
	// seconds := temp % 60
	temp /= 60
	// minutes := temp % 60
	temp /= 60
	// hours := temp

	d_seconds := delta / 1e6
	// d_seconds_total := delta_total / 1e6

	/* find the amount of data transferred (bytes) */
	// data_total := float64(s.param.blockSize) * float64(stats.totalBlocks)
	data_this := float64(s.param.blockSize) * float64(stats.totalBlocks-stats.thisBlocks)
	data_this_rexmit := float64(s.param.blockSize) * float64(stats.thisFlowRetransmitteds)
	// data_this_goodpt := float64(s.param.blockSize) * float64(stats.thisFlowOriginals)

	/* get the current UDP receive error count reported by the operating system */
	stats.thisUdpErrors = tsunami.Get_udp_in_errors()

	/* precalculate some fractions */
	retransmits_fraction := float64(stats.thisRetransmits) / (1.0 + float64(stats.thisRetransmits+stats.totalBlocks-stats.thisBlocks))
	ringfill_fraction := float64(s.tr.ringBuffer.count_data) / MAX_BLOCKS_QUEUED
	// total_retransmits_fraction := float64(stats.totalRetransmits) / float64(stats.totalRetransmits+stats.totalBlocks)

	/* update the rate statistics */
	// incoming transmit rate R = goodput R (Mbit/s) + retransmit R (Mbit/s)
	stats.thisTransmitRate = 8.0 * data_this / float64(d_seconds*u_mega)
	stats.thisRetransmitRate = 8.0 * data_this_rexmit / float64(d_seconds*u_mega)
	// data_total_rate := 8.0 * data_total / float64(d_seconds_total*u_mega)

	fb := float64(s.param.history) / 100.0 // feedback
	ff := 1.0 - fb                         // feedforward

	// IIR filter rate R
	stats.transmitRate = fb*stats.transmitRate + ff*stats.thisRetransmitRate

	// IIR filtered composite error and loss, some sort of knee function
	stats.errorRate = fb*stats.errorRate + ff*500*100*(retransmits_fraction+ringfill_fraction)

	/* send the current error rate information to the server */
	retransmission[0].RequestType = REQUEST_ERROR_RATE
	retransmission[0].ErrorRate = uint32(s.tr.stats.errorRate)
	_, err := s.connection.Write(tsunami.Retransmissions(retransmission).Bytes())
	if err != nil {
		return err
	}

	/* print to the transcript if the user wants */
	if s.param.transcript {
		// session log
	}

	/* reset the statistics for the next interval */
	stats.thisBlocks = stats.totalBlocks
	stats.thisRetransmits = 0
	stats.thisFlowOriginals = 0
	stats.thisFlowRetransmitteds = 0
	stats.thisTime = time.Now()

	/* indicate success */
	return nil
}
