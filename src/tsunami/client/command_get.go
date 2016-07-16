package client

import (
	"errors"
	"fmt"
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

	xfer := &transfer{}
	session.tr = xfer

	rexmit := &(xfer.retransmit)

	var f_total uint64 = 1
	// var f_arrsize uint64 = 0
	multimode := false

	// var wait_u_sec int64 = 1
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
		fmt.Println("elapsed", t1, t2)
		file_count := make([]byte, 10)
		_, err = session.connection.Read(file_count)
		if err != nil {
			return err
		}
		_, err = session.connection.Write([]byte("got size"))

		/* Calculate and convert RTT to u_sec, with +10% margin */
		// d := t2.Sub(t1).Nanoseconds()
		// wait_u_sec = (d + d/10) / 1000

		// f_arrsize, _ = strconv.ParseUint(string(filearray_size), 10, 64)
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

		xfer.ringBuffer = session.NewRingBuffer()

		// local_datagram := make([]byte, 6+session.param.blockSize)

		/* Finish initializing the retransmission object */
		rexmit.tableSize = DEFAULT_TABLE_SIZE
		rexmit.indexMax = 0

		/* we start by expecting block #1 */
		xfer.nextBlock = 1
		xfer.gaplessToBlock = 0

	}

	return nil
}
