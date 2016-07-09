package client

import (
	"fmt"
	"os"
	"time"

	"tsunami"
)

func (s *Session) XsriptOpen() {
	xfer := s.tr
	param := s.param

	fileName := tsunami.MakeTranscriptFileName(time.Now(), "tsuc")

	f, err := os.OpenFile(fileName, os.O_WRONLY|os.O_CREATE, 0)
	if err != nil {
		fmt.Println("Could not create transcript file")
		return
	}
	xfer.transcript = f

	/* write out all the header information */

	fmt.Fprintln(f, "remote_filename =", xfer.remoteFileName)
	fmt.Fprintln(f, "local_filename =", xfer.localFileName)
	fmt.Fprintln(f, "file_size =", xfer.fileSize)
	fmt.Fprintln(f, "block_count =", xfer.blockCount)
	fmt.Fprintln(f, "udp_buffer =", param.udpBuffer)
	fmt.Fprintln(f, "block_size =", param.blockSize)
	fmt.Fprintln(f, "target_rate =", param.targetRate)
	fmt.Fprintln(f, "error_rate =", param.errorRate)
	fmt.Fprintln(f, "slower_num =", param.slowerNum)
	fmt.Fprintln(f, "slower_den =", param.slowerDen)
	fmt.Fprintln(f, "faster_num =", param.fasterNum)
	fmt.Fprintln(f, "faster_den =", param.fasterDen)
	fmt.Fprintln(f, "history =", param.history)
	fmt.Fprintln(f, "lossless =", param.lossless)
	fmt.Fprintln(f, "losswindow =", param.losswindow_ms)
	fmt.Fprintln(f, "blockdump =", param.blockDump)
	fmt.Fprintln(f, "update_period =", tsunami.UPDATE_PERIOD)
	fmt.Fprintln(f, "rexmit_period = ", tsunami.UPDATE_PERIOD)
	fmt.Fprintln(f, "protocol_version", tsunami.PROTOCOL_REVISION)
	fmt.Fprintln(f, "software_version =", tsunami.TSUNAMI_CVS_BUILDNR)
	fmt.Fprintln(f, "ipv6 =", param.ipv6)
	fmt.Fprintln(f)
	f.Sync()
}

func (s *Session) XsriptClose(delta uint64) {
	xfer := s.tr
	param := s.param
	f := xfer.transcript
	if f == nil {
		return
	}

	mb_thru := xfer.stats.totalBlocks * param.blockSize
	mb_good := mb_thru - xfer.stats.totalRecvdRetransmits*param.blockSize
	mb_file := xfer.fileSize
	mb_thru /= (1024.0 * 1024.0)
	mb_good /= (1024.0 * 1024.0)
	mb_file /= (1024.0 * 1024.0)
	secs := float64(delta) / 1e6

	fmt.Fprintln(f, "mbyte_transmitted =", mb_thru)
	fmt.Fprintln(f, "mbyte_usable =", mb_good)
	fmt.Fprintln(f, "mbyte_file =", mb_file)
	fmt.Fprintln(f, "duration =", secs)
	fmt.Fprintln(f, "throughput =", 8.0*float64(mb_thru)/secs)
	fmt.Fprintln(f, "goodput_with_restarts =", 8.0*float64(mb_good)/secs)
	fmt.Fprintln(f, "file_rate =", 8.0*float64(mb_file)/secs)
	f.Close()
}

func (s *Session) XsriptDataStart(t time.Time) {
	s.xsriptDataSnap("START", t)
}

func (s *Session) XsriptDataLog(logLine string) {
	s.xsriptDataWrite(logLine)
}

func (s *Session) XsriptDataStop(t time.Time) {
	s.xsriptDataSnap("STOP", t)
}

func (s *Session) xsriptDataWrite(str string) {
	f := s.tr.transcript
	if f == nil {
		return
	}
	fmt.Fprint(f, "%s", str)
	f.Sync()
}

func (s *Session) xsriptDataSnap(tag string, t time.Time) {
	str := fmt.Sprintf("%s %d.%06d\n", tag, t.Unix(), t.UnixNano()%1e9)
	s.xsriptDataWrite(str)
}
