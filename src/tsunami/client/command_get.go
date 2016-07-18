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

	if remotePath == "*" {
		multimode = true
		file_names, err := session.multiFileRequest()
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
		err := session.getSingleFile()
		if err != nil {
			break
		}
	}

	return nil
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
	return nil
}

/*------------------------------------------------------------------------
 * void *disk_thread(void *arg);
 *
 * This is the thread that takes care of saved received blocks to disk.
 * It runs until the network thread sends it a datagram with a block
 * number of 0.  The return value has no meaning.
 *------------------------------------------------------------------------*/
func (session *Session) disk_thread() {
	/* while the world is turning */
	for {
		/* get another block */
		datagram := session.tr.ringBuffer.peek()
		blockIndex := binary.BigEndian.Uint32(datagram[:4])
		// blockType := binary.BigEndian.Uint16(datagram[4:6])

		/* quit if we got the mythical 0 block */
		if blockIndex == 0 {
			fmt.Println("!!!!")
			return
		}

		/* save it to disk */
		err := session.accept_block(blockIndex, datagram[6:])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Block accept failed")
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
 * void dump_blockmap(const char *postfix, const ttp_transfer_t *xfer)
 *
 * Writes the current bitmap of received block accounting into
 * a file named like the transferred file but with an extra postfix.
 *------------------------------------------------------------------------*/
func (xfer *transfer) dump_blockmap(postfix string) {
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
