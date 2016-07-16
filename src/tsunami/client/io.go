package client

import (
	"fmt"
	"os"
)

/*------------------------------------------------------------------------
 * int accept_block(ttp_session_t *session,
 *                  u_int32_t blockIndex, u_char *block);
 *
 * Accepts the given block of data, which involves writing the block
 * to disk.  Returns 0 on success and nonzero on failure.
 *------------------------------------------------------------------------*/
func (session *Session) accept_block(blockIndex uint32, block []byte) error {

	transfer := session.tr
	blockSize := session.param.blockSize
	var writeSize uint32
	if blockIndex == transfer.blockCount {
		writeSize = uint32(transfer.fileSize % uint64(blockSize))
		if writeSize == 0 {
			writeSize = blockSize
		}
	} else {
		writeSize = blockSize
	}

	_, err := transfer.localFile.Seek(int64(blockSize*(blockIndex-1)), os.SEEK_SET)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not seek at block %d of file", blockIndex)
		return err
	}

	_, err = transfer.localFile.Write(block)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not write block %d of file", blockIndex)
		return err
	}

	return nil
}
