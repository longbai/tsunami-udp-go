package server

import (
	"encoding/binary"
	"os"

	"tsunami"
)

/*------------------------------------------------------------------------
 * int build_datagram(ttp_session_t *session, u_int32_t block_index,
 *                    u_int16_t block_type, u_char *datagram);
 *
 * Constructs to hold the given block of data, with the given type
 * stored in it.  The format of the datagram is:
 *
 *     32                    0
 *     +---------------------+
 *     |     block_number    |
 *     +----------+----------+
 *     |   type   |   data   |
 *     +----------+     :    |
 *     :     :          :    :
 *     +---------------------+
 *
 * The datagram is stored in the given buffer, which must be at least
 * six bytes longer than the block size for the transfer.  Returns 0 on
 * success and non-zero on failure.
 *------------------------------------------------------------------------*/

func (session *Session) buildDatagram(block_index uint32,
	block_type uint16, datagram []byte) error {

	/* move the file pointer to the appropriate location */
	if block_index != (session.last_block + 1) {
		_, err := session.transfer.file.Seek(int64(session.parameter.block_size*(block_index-1)), os.SEEK_SET)
		if err != nil {
			return err
		}
	}

	/* try to read in the block */
	_, err := session.transfer.file.Read(datagram[6:])
	if err != nil {
		return tsunami.Warn("Could not read block #", block_index, err)
	}

	/* build the datagram header */
	binary.BigEndian.PutUint32(datagram, block_index)
	binary.BigEndian.PutUint16(datagram[4:], block_type)

	session.last_block = block_index
	return nil
}
