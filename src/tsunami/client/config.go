package client

import (
	"tsunami"
)

/*------------------------------------------------------------------------
 * Global constants.
 *------------------------------------------------------------------------*/

const SCREEN_MODE = 0 /* screen-based output mode                     */
const LINE_MODE = 1   /* line-based (vmstat-like) output mode         */

const DEFAULT_BLOCK_SIZE = 1024                 /* default size of a single file block          */
const DEFAULT_TABLE_SIZE = 4096                 /* initial size of the retransmission table     */
const DEFAULT_SERVER_NAME = "localhost"         /* default name of the remote server            */
const DEFAULT_SERVER_PORT = tsunami.TS_TCP_PORT /* default TCP port of the remote server        */
const DEFAULT_CLIENT_PORT = tsunami.TS_UDP_PORT /* default UDP port of the client               */
const DEFAULT_UDP_BUFFER = 20000000             /* default size of the UDP receive buffer       */
const DEFAULT_VERBOSE_YN = true                 /* the default verbosity setting                */
const DEFAULT_TRANSCRIPT_YN = false             /* the default transcript setting               */
const DEFAULT_IPV6_YN = false                   /* the default IPv6 setting                     */
const DEFAULT_OUTPUT_MODE = LINE_MODE           /* the default output mode (SCREEN or LINE)     */
const DEFAULT_RATE_ADJUST = false               /* the default for remembering achieved rate    */
const DEFAULT_TARGET_RATE = 650000000           /* the default target rate (in bps)             */
const DEFAULT_ERROR_RATE = 7500                 /* the default threshhold error rate (% x 1000) */
const DEFAULT_SLOWER_NUM = 25                   /* default numerator in the slowdown factor     */
const DEFAULT_SLOWER_DEN = 24                   /* default denominator in the slowdown factor   */
const DEFAULT_FASTER_NUM = 5                    /* default numerator in the speedup factor      */
const DEFAULT_FASTER_DEN = 6                    /* default denominator in the speedup factor    */
const DEFAULT_HISTORY = 25                      /* default percentage of history in rates       */
const DEFAULT_NO_RETRANSMIT = false             /* on default use retransmission                */
const DEFAULT_LOSSLESS = true                   /* default to lossless transfer                 */
const DEFAULT_LOSSWINDOW_MS = 1000              /* default time window (msec) for semi-lossless */

const DEFAULT_BLOCKDUMP = false /* on default do not write bitmap dump to file  */

const MAX_COMMAND_LENGTH = 1024 /* maximum length of a single command           */

/* Tsunami transfer protocol parameters */
// typedef struct {
//     char               *server_name;              /* the name of the host running tsunamid       */
//     u_int16_t           server_port;              /* the TCP port on which the server listens    */
//     u_int16_t           client_port;              /* the UDP port on which the client receives   */
//     u_int32_t           udp_buffer;               /* the size of the UDP receive buffer in bytes */
//     u_char              verbose_yn;               /* 1 for verbose mode, 0 for quiet             */
//     u_char              transcript_yn;            /* 1 for transcripts on, 0 for no transcript   */
//     u_char              ipv6_yn;                  /* 1 for IPv6, 0 for IPv4                      */
//     u_char              output_mode;              /* either SCREEN_MODE or LINE_MODE             */
//     u_int32_t           block_size;               /* the size of each block (in bytes)           */
//     u_int32_t           target_rate;              /* the transfer rate that we're targetting     */
//     u_char              rate_adjust;              /* 1 for adjusting target to achieved rate     */
//     u_int32_t           error_rate;               /* the threshhold error rate (in % x 1000)     */
//     u_int16_t           slower_num;               /* the numerator of the increase-IPD factor    */
//     u_int16_t           slower_den;               /* the denominator of the increase-IPD factor  */
//     u_int16_t           faster_num;               /* the numerator of the decrease-IPD factor    */
//     u_int16_t           faster_den;               /* the denominator of the decrease-IPD factor  */
//     u_int16_t           history;                  /* percentage of history to keep in rates      */
//     u_char              lossless;                 /* 1 for lossless, 0 for data rate priority    */
//     u_int32_t           losswindow_ms;            /* data rate priority: time window for re-tx's */
//     u_char              blockdump;                /* 1 to write received block bitmap to a file  */
//     char                *passphrase;              /* the passphrase to use for authentication    */
//     char                *ringbuf;                 /* Pointer to ring buffer start                */
// } ttp_parameter_t;

type Parameter struct {
	serverName string
	serverPort uint16
	clientPort uint16
	udpBuffer  uint32

	verbose    bool
	transcript bool
	ipv6       bool
	outputMode uint32
	blockSize  uint32

	targetRate uint32
	rateAdjust bool
	errorRate  uint32

	slowerNum       uint16
	slowerDen       uint16
	fasterNum       uint16
	fasterDen       uint16
	history         uint16
	lossless        bool
	losswindow_ms   uint32
	blockDump       bool
	passphrase      string
	ringBufferIndex uint32
}

func NewParameter() *Parameter {
	parameter := Parameter{}
	/* fill the fields with their defaults */
	parameter.blockSize = DEFAULT_BLOCK_SIZE
	parameter.serverName = DEFAULT_SERVER_NAME
	parameter.serverPort = DEFAULT_SERVER_PORT
	parameter.clientPort = DEFAULT_CLIENT_PORT
	parameter.udpBuffer = DEFAULT_UDP_BUFFER
	parameter.verbose = DEFAULT_VERBOSE_YN
	parameter.transcript = DEFAULT_TRANSCRIPT_YN
	parameter.ipv6 = DEFAULT_IPV6_YN
	parameter.outputMode = DEFAULT_OUTPUT_MODE
	parameter.targetRate = DEFAULT_TARGET_RATE
	parameter.rateAdjust = DEFAULT_RATE_ADJUST
	parameter.errorRate = DEFAULT_ERROR_RATE
	parameter.slowerNum = DEFAULT_SLOWER_NUM
	parameter.slowerDen = DEFAULT_SLOWER_DEN
	parameter.fasterNum = DEFAULT_FASTER_NUM
	parameter.fasterDen = DEFAULT_FASTER_DEN
	parameter.history = DEFAULT_HISTORY
	parameter.lossless = DEFAULT_LOSSLESS
	parameter.losswindow_ms = DEFAULT_LOSSWINDOW_MS
	parameter.blockDump = DEFAULT_BLOCKDUMP
	return &parameter
}
