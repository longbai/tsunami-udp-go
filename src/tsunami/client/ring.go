package client

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
)

const EMPTY = -1

/* ring buffer for queuing blocks to be written to disk */
type ringBuffer struct {
	datagrams      []byte     /* the collection of queued datagrams          */
	datagramSize   int        /* the size of a single datagram               */
	baseData       int        /* the index of the first slot with data       */
	countData      int        /* the number of slots in use for data         */
	countReserved  int        /* the number of slots reserved without data   */
	mutex          sync.Mutex /* a mutex to guard the ring buffer            */
	dataReadyCond  *sync.Cond /* condition variable to indicate data ready   */
	dataReady      bool       /* nonzero when data is ready, else 0          */
	spaceReadyCond *sync.Cond /* condition variable to indicate space ready  */
	spaceReady     bool       /* nonzero when space is available, else 0     */
}

/*------------------------------------------------------------------------
 * int ring_full(ring_buffer *ring);
 *
 * Returns non-zero if ring is full.
 *------------------------------------------------------------------------*/
func (ring *ringBuffer) isFull() bool {
	ring.mutex.Lock()
	full := !ring.spaceReady
	ring.mutex.Unlock()
	return full
}

/*------------------------------------------------------------------------
 * int ring_cancel(ring_buffer *ring);
 *
 * Cancels the reservation for the slot that was most recently reserved.
 * Returns 0 on success and nonzero on error.
 *------------------------------------------------------------------------*/
func (ring *ringBuffer) cancel() error {
	ring.mutex.Lock()
	defer ring.mutex.Unlock()

	/* convert the reserved slot into space */
	ring.countReserved--
	if ring.countReserved < 0 {
		return errors.New("Attempt made to cancel unreserved slot in ring buffer")
	}

	/* signal that space is available */
	ring.spaceReady = true
	ring.spaceReadyCond.Signal()
	return nil
}

/*------------------------------------------------------------------------
 * int ring_confirm(ring_buffer *ring);
 *
 * Confirms that data is now available in the slot that was most
 * recently reserved.  This data will be handled by the disk thread.
 * Returns 0 on success and nonzero on error.
 *------------------------------------------------------------------------*/
func (ring *ringBuffer) confirm() error {
	ring.mutex.Lock()
	defer ring.mutex.Unlock()

	/* convert the reserved slot into data */
	ring.countData++
	ring.countReserved--
	if ring.countReserved < 0 {
		return errors.New("Attempt made to confirm unreserved slot in ring buffer")
	}

	/* signal that data is available */
	ring.dataReady = true
	ring.dataReadyCond.Signal()
	return nil
}

/*------------------------------------------------------------------------
 * ring_buffer_t *ring_create(ttp_session_t *session);
 *
 * Creates the ring buffer data structure for a Tsunami transfer and
 * returns a pointer to the new data structure.  Returns NULL if
 * allocation and initialization failed.  The new ring buffer will hold
 * ([6 + block_size] * MAX_BLOCKS_QUEUED datagrams.
 *------------------------------------------------------------------------*/
func (session *Session) NewRingBuffer() *ringBuffer {
	ring := new(ringBuffer)

	ring.datagramSize = 6 + int(session.param.blockSize)
	ring.datagrams = make([]byte, ring.datagramSize*MAX_BLOCKS_QUEUED)

	ring.dataReady = false
	ring.spaceReady = true

	/* initialize the indices */
	ring.countData = 0
	ring.countReserved = 0
	ring.baseData = 0

	ring.dataReadyCond = sync.NewCond(&ring.mutex)
	ring.spaceReadyCond = sync.NewCond(&ring.mutex)

	return ring
}

/*------------------------------------------------------------------------
 * int ring_destroy(ring_buffer_t *ring);
 *
 * Destroys the ring buffer data structure for a Tsunami transfer,
 * including the mutex and condition variables.  Returns 0 on success
 * and nonzero on failure.
 *------------------------------------------------------------------------*/
func (ring *ringBuffer) destroy() {
	// do nothing in go
}

/*------------------------------------------------------------------------
 * int ring_dump(ring_buffer_t *ring, FILE *out);
 *
 * Dumps the current contents of the ring buffer to the given output
 * stream.  Returns zero on success and non-zero on error.
 *------------------------------------------------------------------------*/
func (ring *ringBuffer) dump(out io.Writer) {
	ring.mutex.Lock()
	/* print out the top-level fields */
	fmt.Fprintln(out, "datagram_size  = ", ring.datagramSize)
	fmt.Fprintln(out, "base_data      = ", ring.baseData)
	fmt.Fprintln(out, "count_data     = ", ring.countData)
	fmt.Fprintln(out, "count_reserved = ", ring.countReserved)
	fmt.Fprintln(out, "data_ready     = ", ring.dataReady)
	fmt.Fprintln(out, "space_ready    = ", ring.spaceReady)

	/* print out the block list */
	fmt.Fprint(out, "block list     = [")
	for index := ring.baseData; index < ring.baseData+ring.countData; index++ {
		offset := ((index % MAX_BLOCKS_QUEUED) * ring.datagramSize)
		d := binary.BigEndian.Uint32(ring.datagrams[offset:])
		fmt.Fprint(out, d)
	}
	fmt.Fprintln(out, "]")
	ring.mutex.Unlock()
}

/*------------------------------------------------------------------------
 * u_char *ring_peek(ring_buffer_t *ring);
 *
 * Attempts to return a pointer to the datagram at the head of the ring.
 * This will block if the ring is currently empty.  Returns NULL on error.
 *------------------------------------------------------------------------*/
func (ring *ringBuffer) ring_peek() []byte {
	ring.mutex.Lock()

	/* wait for the data-ready variable to make us happy */
	for !ring.dataReady {
		ring.dataReadyCond.Wait()
	}
	ring.mutex.Unlock()
	/* find the address we want */
	address := ring.datagrams[ring.datagramSize+ring.baseData:]
	return address
}

/*------------------------------------------------------------------------
 * int ring_pop(ring_buffer_t *ring);
 *
 * Attempts to remove a datagram from the head of the ring.  This will
 * block if the ring is currently empty.  Returns 0 on success and
 * nonzero on error.
 *------------------------------------------------------------------------*/
func (ring *ringBuffer) pop() {
	ring.mutex.Lock()

	/* wait for the data-ready variable to make us happy */
	for !ring.dataReady {
		ring.dataReadyCond.Wait()
	}

	/* perform the pop operation */
	ring.baseData = (ring.baseData + 1) % MAX_BLOCKS_QUEUED
	ring.countData--
	if ring.countData == 0 {
		ring.dataReady = false
	}
	ring.spaceReady = true

	ring.mutex.Unlock()
}

/*------------------------------------------------------------------------
 * u_char *ring_reserve(ring_buffer_t *ring);
 *
 * Reserves a slot in the ring buffer for the next datagram.  A pointer
 * to the memory that should be used to store the datagram is returned.
 * This will block if no space is available in the ring buffer.  Returns
 * NULL on error.
 *------------------------------------------------------------------------*/
func (ring *ringBuffer) reserve() ([]byte, error) {
	ring.mutex.Lock()
	defer ring.mutex.Unlock()

	/* figure out which slot comes next */
	next := (ring.baseData + ring.countData + ring.countReserved) % MAX_BLOCKS_QUEUED

	/* wait for the space-ready variable to make us happy */
	for !ring.spaceReady {
		fmt.Println("FULL! -- ring_reserve() blocking.")
		fmt.Printf("space_ready = %d, data_ready = %d\n", ring.spaceReady, ring.dataReady)
		ring.spaceReadyCond.Wait()
	}

	/* perform the reservation */
	ring.countReserved++
	if ring.countReserved > 1 {
		return nil, errors.New("Attempt made to reserve two slots in ring buffer")
	}

	if ((next + 1) % MAX_BLOCKS_QUEUED) == ring.baseData {
		ring.spaceReady = false
	}

	/* find the address we want */
	address := ring.datagrams[next*ring.datagramSize:]

	return address, nil
}
