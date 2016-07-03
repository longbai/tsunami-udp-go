package server

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	opt "github.com/pborman/getopt"

	"tsunami"
)

/*------------------------------------------------------------------------
 * Global constants.
 *------------------------------------------------------------------------*/

const DEFAULT_BLOCK_SIZE = 1024 /* default size of a single file block     */
// const DEFAULT_SECRET = tsunami.DEFAULT_SECRET /* default shared secret          */
// const DEFAULT_TCP_PORT = tsunami.TS_TCP_PORT  /* default TCP port to listen on          */
const DEFAULT_UDP_BUFFER = 20000000  /* default size of the UDP transmit buffer */
const DEFAULT_VERBOSE_YN = true      /* the default verbosity setting           */
const DEFAULT_TRANSCRIPT_YN = false  /* the default transcript setting          */
const DEFAULT_IPV6_YN = false        /* the default IPv6 setting                */
const DEFAULT_HEARTBEAT_TIMEOUT = 15 /* the timeout to disconnect after no client feedback */

/* Tsunami transfer protocol parameters */
type Parameter struct {
	epoch          time.Time /* the Unix epoch used to identify this run   */
	verbose        bool      /* verbose mode (0=no, 1=yes)                 */
	transcript     bool      /* transcript mode (0=no, 1=yes)              */
	ipv6           bool      /* IPv6 mode (0=no, 1=yes)                    */
	tcp_port       uint16    /* TCP port number for listening on           */
	udp_buffer     uint32    /* size of the UDP send buffer in bytes       */
	hb_timeout     uint16    /* the client heartbeat timeout               */
	secret         string    /* the shared secret for users to prove       */
	client         string    /* the alternate client IP to stream to       */
	finishhook     string    /* program to run after successful copy       */
	allhook        string    /* program to run to get listing of files for "get *" */
	block_size     uint32    /* the size of each block (in bytes)          */
	file_size      uint64    /* the total file size (in bytes)             */
	block_count    uint32    /* the total number of blocks in the file     */
	target_rate    uint32    /* the transfer rate that we're targetting    */
	error_rate     uint32    /* the threshhold error rate (in % x 1000)    */
	ipd_time       uint32    /* the inter-packet delay in usec             */
	slower_num     uint16    /* the numerator of the increase-IPD factor   */
	slower_den     uint16    /* the denominator of the increase-IPD factor */
	faster_num     uint16    /* the numerator of the decrease-IPD factor   */
	faster_den     uint16    /* the denominator of the decrease-IPD factor */
	ringbuf        []byte    /* Pointer to ring buffer start               */
	fileout        uint16    /* Do we store the data to file?              */
	slotnumber     int       /* Slot number for distributed transfer       */
	totalslots     int       /* How many slots do we have?                 */
	samplerate     int       /* Sample rate in MHz (optional)              */
	file_names     []string  /* Store the local file_names on server       */
	file_sizes     []uint64  /* Store the local file sizes on server       */
	file_name_size uint16    /* Store the total size of the array          */
	total_files    uint16    /* Store the total number of served files     */
	wait_u_sec     uint32
}

func NewParameter() *Parameter {
	parameter := Parameter{}
	parameter.block_size = DEFAULT_BLOCK_SIZE
	parameter.secret = tsunami.DEFAULT_SECRET
	parameter.client = ""
	parameter.tcp_port = tsunami.TS_TCP_PORT
	parameter.udp_buffer = DEFAULT_UDP_BUFFER
	parameter.hb_timeout = DEFAULT_HEARTBEAT_TIMEOUT
	parameter.verbose = DEFAULT_VERBOSE_YN
	parameter.transcript = DEFAULT_TRANSCRIPT_YN
	parameter.ipv6 = DEFAULT_IPV6_YN
	return &parameter
}

/*------------------------------------------------------------------------
 * void process_options();
 *
 * Processes the command-line options and sets the protocol parameters
 * as appropriate.
 *------------------------------------------------------------------------*/
func ProcessOptions() *Parameter {
	verbose := opt.BoolLong("verbose", 'v',
		"turns on verbose output mode, deafult off")
	transcript := opt.BoolLong("transcript", 't',
		"turns on transcript mode for statistics recording, deafult off")
	ipv6 := opt.BoolLong("v6", '6',
		"operates using IPv6 instead of (not in addition to!) IPv4")
	deafultHelp := fmt.Sprintf(
		"specifies which TCP port on which to listen to incoming connections, default %d",
		tsunami.TS_TCP_PORT)
	port := opt.Uint16Long("port", 'p', tsunami.TS_TCP_PORT, deafultHelp)
	secret := opt.StringLong("secret", 's', tsunami.DEFAULT_SECRET,
		"specifies the shared secret for the client and server")

	deafultHelp = fmt.Sprintf(
		"specifies the desired size for UDP socket send buffer (in bytes), default %d",
		DEFAULT_UDP_BUFFER)
	buffer := opt.Uint32Long("buffer", 'b', DEFAULT_UDP_BUFFER, deafultHelp)

	deafultHelp = fmt.Sprintf(
		"specifies the timeout in seconds for disconnect after client heartbeat lost, default %d",
		DEFAULT_HEARTBEAT_TIMEOUT)
	hbtimeout := opt.Uint16Long("hbtimeout", 'h', DEFAULT_HEARTBEAT_TIMEOUT, deafultHelp)
	client := opt.StringLong("client", 'c',
		"specifies an alternate client IP or host where to send data")
	finishhook := opt.StringLong("finishhook", 'f',
		"run command on transfer completion, file name is appended automatically")
	allhook := opt.StringLong("allhook", 'a',
		"run command on 'get *' to produce a custom file list for client downloads")
	opt.Parse()

	parameter := NewParameter()

	parameter.verbose = *verbose
	parameter.transcript = *transcript
	parameter.ipv6 = *ipv6
	parameter.tcp_port = *port
	parameter.secret = *secret
	parameter.udp_buffer = *buffer
	parameter.hb_timeout = *hbtimeout
	parameter.client = *client
	parameter.finishhook = *finishhook
	parameter.allhook = *allhook

	files := opt.Args()
	parameter.file_names = files
	parameter.file_sizes = make([]uint64, len(files))
	for i, v := range files {
		stat, err := os.Stat(v)
		if err != nil {
			fmt.Fprintln(os.Stderr, v, err)
		} else {
			size := stat.Size()
			parameter.file_sizes[i] = uint64(size)
			fmt.Fprintf(os.Stderr, " %3d)   %-20s  %d bytes\n", i+1, v, size)
		}
	}

	parameter.VerboseArg("")
	return parameter
}

func (param *Parameter) VerboseArg(prompt string) {
	if !param.verbose {
		return
	}
	if prompt != "" {
		fmt.Fprintln(os.Stderr, prompt)
	}

	fmt.Fprintln(os.Stderr, "Block size:", param.block_size)
	fmt.Fprintln(os.Stderr, "Buffer size:", param.udp_buffer)
	fmt.Fprintln(os.Stderr, "Port:", param.tcp_port)
}

func (param *Parameter) FinishHook(file string) error {
	if param.finishhook != "" {
		fmt.Fprintln(os.Stderr, "Executing:", param.finishhook, file)
		cmd := exec.Command(param.finishhook, file)
		return cmd.Run()
	}
	return nil
}
