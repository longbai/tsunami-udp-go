package server

import (
	"tsunami"
)

/*------------------------------------------------------------------------
 * Global constants.
 *------------------------------------------------------------------------*/

const DEFAULT_BLOCK_SIZE = 1024               /* default size of a single file block     */
const DEFAULT_SECRET = tsunami.DEFAULT_SECRET /* default shared secret          */
const DEFAULT_TCP_PORT = tsunami.TS_TCP_PORT  /* default TCP port to listen on          */
const DEFAULT_UDP_BUFFER = 20000000           /* default size of the UDP transmit buffer */
const DEFAULT_VERBOSE_YN = true               /* the default verbosity setting           */
const DEFAULT_TRANSCRIPT_YN = false           /* the default transcript setting          */
const DEFAULT_IPV6_YN = false                 /* the default IPv6 setting                */
const DEFAULT_HEARTBEAT_TIMEOUT = 15          /* the timeout to disconnect after no client feedback */

/* Tsunami transfer protocol parameters */
type Parameter struct {
	epoch          uint32   /* the Unix epoch used to identify this run   */
	verbose        bool     /* verbose mode (0=no, 1=yes)                 */
	transcript     bool     /* transcript mode (0=no, 1=yes)              */
	ipv6           bool     /* IPv6 mode (0=no, 1=yes)                    */
	tcp_port       uint16   /* TCP port number for listening on           */
	udp_buffer     uint32   /* size of the UDP send buffer in bytes       */
	hb_timeout     uint16   /* the client heartbeat timeout               */
	secret         string   /* the shared secret for users to prove       */
	client         string   /* the alternate client IP to stream to       */
	finishhook     string   /* program to run after successful copy       */
	allhook        string   /* program to run to get listing of files for "get *" */
	block_size     uint32   /* the size of each block (in bytes)          */
	file_size      uint64   /* the total file size (in bytes)             */
	block_count    uint32   /* the total number of blocks in the file     */
	target_rate    uint32   /* the transfer rate that we're targetting    */
	error_rate     uint32   /* the threshhold error rate (in % x 1000)    */
	ipd_time       uint32   /* the inter-packet delay in usec             */
	slower_num     uint16   /* the numerator of the increase-IPD factor   */
	slower_den     uint16   /* the denominator of the increase-IPD factor */
	faster_num     uint16   /* the numerator of the decrease-IPD factor   */
	faster_den     uint16   /* the denominator of the decrease-IPD factor */
	ringbuf        []byte   /* Pointer to ring buffer start               */
	fileout        uint16   /* Do we store the data to file?              */
	slotnumber     int      /* Slot number for distributed transfer       */
	totalslots     int      /* How many slots do we have?                 */
	samplerate     int      /* Sample rate in MHz (optional)              */
	file_names     []string /* Store the local file_names on server       */
	file_sizes     []uint64 /* Store the local file sizes on server       */
	file_name_size uint16   /* Store the total size of the array          */
	total_files    uint16   /* Store the total number of served files     */
	wait_u_sec     uint32
}

func NewParameter() *Parameter {
	parameter := Parameter{}
	parameter.block_size = DEFAULT_BLOCK_SIZE
	parameter.secret = DEFAULT_SECRET
	parameter.client = ""
	parameter.tcp_port = DEFAULT_TCP_PORT
	parameter.udp_buffer = DEFAULT_UDP_BUFFER
	parameter.hb_timeout = DEFAULT_HEARTBEAT_TIMEOUT
	parameter.verbose = DEFAULT_VERBOSE_YN
	parameter.transcript = DEFAULT_TRANSCRIPT_YN
	parameter.ipv6 = DEFAULT_IPV6_YN
	return &parameter
}
