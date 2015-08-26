package client

import (
	"tsunami"
)

/*------------------------------------------------------------------------
 * Global constants.
 *------------------------------------------------------------------------*/

const SCREEN_MODE = 0 /* screen-based output mode                     */
const LINE_MODE = 1   /* line-based (vmstat-like) output mode         */

const DEFAULT_BLOCK_SIZE = 1024         /* default size of a single file block          */
const DEFAULT_TABLE_SIZE = 4096         /* initial size of the retransmission table     */
const DEFAULT_SERVER_NAME = "localhost" /* default name of the remote server            */
const DEFAULT_SECRET = tsunami.DEFAULT_SECRET
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

type Parameter struct {
	serverName      string /* the name of the host running tsunamid       */
	serverPort      uint16 /* the TCP port on which the server listens    */
	clientPort      uint16 /* the UDP port on which the client receives   */
	udpBuffer       uint32 /* the size of the UDP receive buffer in bytes */
	verbose         bool   /* 1 for verbose mode, 0 for quiet             */
	transcript      bool   /* 1 for transcripts on, 0 for no transcript   */
	ipv6            bool   /* 1 for IPv6, 0 for IPv4                      */
	outputMode      uint32 /* either SCREEN_MODE or LINE_MODE             */
	blockSize       uint32 /* the size of each block (in bytes)           */
	targetRate      uint32 /* the transfer rate that we're targetting     */
	rateAdjust      bool   /* 1 for adjusting target to achieved rate     */
	errorRate       uint32 /* the threshhold error rate (in % x 1000)     */
	slowerNum       uint16 /* the numerator of the increase-IPD factor    */
	slowerDen       uint16 /* the denominator of the increase-IPD factor  */
	fasterNum       uint16 /* the numerator of the decrease-IPD factor    */
	fasterDen       uint16 /* the denominator of the decrease-IPD factor  */
	history         uint16 /* percentage of history to keep in rates      */
	lossless        bool   /* 1 for lossless, 0 for data rate priority    */
	losswindow_ms   uint32 /* data rate priority: time window for re-tx's */
	blockDump       bool   /* 1 to write received block bitmap to a file  */
	passphrase      string /* the passphrase to use for authentication    */
	ringBufferIndex uint32 /* Pointer to ring buffer start                */
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
	parameter.passphrase = DEFAULT_SECRET
	return &parameter
}
