package client

import (
	"sync"
)

/* ring buffer for queuing blocks to be written to disk */
// typedef struct {
//     u_char             *datagrams;                /* the collection of queued datagrams          */
//     int                 datagram_size;            /* the size of a single datagram               */
//     int                 base_data;                /* the index of the first slot with data       */
//     int                 count_data;               /* the number of slots in use for data         */
//     int                 count_reserved;           /* the number of slots reserved without data   */
//     pthread_mutex_t     mutex;                    /* a mutex to guard the ring buffer            */
//     pthread_cond_t      data_ready_cond;          /* condition variable to indicate data ready   */
//     int                 data_ready;               /* nonzero when data is ready, else 0          */
//     pthread_cond_t      space_ready_cond;         /* condition variable to indicate space ready  */
//     int                 space_ready;              /* nonzero when space is available, else 0     */
// } ring_buffer_t;

type ring_buffer struct {
	datagrams      []byte     /* the collection of queued datagrams          */
	datagram_size  int        /* the size of a single datagram               */
	base_data      int        /* the index of the first slot with data       */
	count_data     int        /* the number of slots in use for data         */
	count_reserved int        /* the number of slots reserved without data   */
	mutex          sync.Mutex /* a mutex to guard the ring buffer            */
	// pthread_cond_t      data_ready_cond;          /* condition variable to indicate data ready   */
	data_ready int /* nonzero when data is ready, else 0          */
	// pthread_cond_t      space_ready_cond;         /* condition variable to indicate space ready  */
	space_ready int /* nonzero when space is available, else 0     */
}
