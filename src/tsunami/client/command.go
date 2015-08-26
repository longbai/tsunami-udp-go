package client

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path"
	"strconv"
	"time"

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

	secret := parameter.passphrase

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
func CommandGet(remotePath string, localPath string, session *Session) error {
	if remotePath == "" {
		return errors.New("Invalid command syntax (use 'help get' for details)")
	}
	if session == nil || session.connection == nil {
		return errors.New("Not connected to a Tsunami server")
	}

	xfer := &transfer{}
	session.tr = xfer

	rexmit := &(xfer.retransmit)

	var f_total uint64 = 1
	var f_arrsize uint64 = 0
	multimode := false

	var wait_u_sec int64 = 1
	var file_names []string

	if remotePath == "*" {
		multimode = true
		fmt.Println("Requesting all available files")
		/* Send request and try to calculate the RTT from client to server */
		t1 := time.Now()
		_, err := session.connection.Write([]byte("*\n"))
		if err != nil {
			return err
		}
		filearray_size := make([]byte, 10)
		_, err = session.connection.Read(filearray_size)
		if err != nil {
			return err
		}
		t2 := time.Now()
		file_count := make([]byte, 10)
		_, err = session.connection.Read(file_count)
		if err != nil {
			return err
		}
		_, err = session.connection.Write([]byte("got size"))

		/* Calculate and convert RTT to u_sec, with +10% margin */
		d := t2.Sub(t1).Nanoseconds()
		wait_u_sec = (d + d/10) / 1000

		f_arrsize, _ = strconv.ParseUint(string(filearray_size), 10, 64)
		f_total, _ = strconv.ParseUint(string(file_count), 10, 64)
		if f_total <= 0 {
			/* get the \x008 failure signal */
			dummy := make([]byte, 1)
			session.connection.Read(dummy)
			return errors.New("Server advertised no files to get")
		}
		fmt.Printf("\nServer is sharing %v files\n", f_total)

		/* Read the file list */
		file_names = make([]string, f_total)

		fmt.Printf("Multi-GET of %v files:\n", f_total)
		for i := 0; i < int(f_total); i++ {
			tmpname, err := tsunami.ReadLine(session.connection, 1024)
			if err != nil {
				return err
			}
			file_names[i] = tmpname
			fmt.Print(tmpname)
		}
		session.connection.Write([]byte("got list"))
		fmt.Println("")
	}

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
		/* negotiate the file request with the server */
		if err := session.ttp_open_transfer(xfer.remoteFileName, xfer.localFileName); err != nil {
			return errors.New(fmt.Sprint("File transfer request failed", err))
		}
		if err := session.ttp_open_port(); err != nil {
			return err
		}

		rexmit.table = make([]uint32, DEFAULT_TABLE_SIZE)
		xfer.received = make([]byte, xfer.blockCount/8+2)

		xfer.ringBuffer = ring_create(session)

		local_datagram := make([]byte, 6+session.param.blockSize)

		/* Finish initializing the retransmission object */
		rexmit.tableSize = DEFAULT_TABLE_SIZE
		rexmit.indexMax = 0

		/* we start by expecting block #1 */
		xfer.nextBlock = 1
		xfer.gaplessToBlock = 0

	}

	return nil
}

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
