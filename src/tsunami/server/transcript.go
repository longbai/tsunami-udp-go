package server

import (
	"fmt"
	"os"
	"time"

	"tsunami"
)

func (s *Session) XsriptOpen() {
	xfer := s.transfer
	param := s.parameter

	fileName := tsunami.MakeTranscriptFileName(param.epoch, "tsus")

	f, err := os.OpenFile(fileName, os.O_WRONLY|os.O_CREATE, 0)
	if err != nil {
		fmt.Println("Could not create transcript file")
		return
	}
	xfer.transcript = f

	/* write out all the header information */

	fmt.Fprintln(f, "filename =", xfer.filename)
	fmt.Fprintln(f, "file_size =", param.file_size)
	fmt.Fprintln(f, "block_count =", param.block_count)
	fmt.Fprintln(f, "udp_buffer =", param.udp_buffer)
	fmt.Fprintln(f, "block_size =", param.block_size)
	fmt.Fprintln(f, "target_rate =", param.target_rate)
	fmt.Fprintln(f, "error_rate =", param.error_rate)
	fmt.Fprintln(f, "slower_num =", param.slower_num)
	fmt.Fprintln(f, "slower_den =", param.slower_den)
	fmt.Fprintln(f, "faster_num =", param.faster_num)
	fmt.Fprintln(f, "faster_den =", param.faster_den)
	fmt.Fprintln(f, "ipd_time =", param.ipd_time)
	fmt.Fprintln(f, "ipd_current =", xfer.ipd_current)
	fmt.Fprintln(f, "protocol_version =", tsunami.PROTOCOL_REVISION)
	fmt.Fprintln(f, "software_version =", tsunami.TSUNAMI_CVS_BUILDNR)
	fmt.Fprintln(f, "ipv6 =", param.ipv6)
	fmt.Fprintln(f)
	f.Sync()
}

func (s *Session) XsriptClose(delta uint64) {
	xfer := s.transfer
	param := s.parameter
	f := xfer.transcript
	if f == nil {
		return
	}

	fmt.Fprintln(f, "mb_transmitted = %0.2f\n", param.file_size/(1024.0*1024.0))
	fmt.Fprintln(f, "duration = %0.2f\n", delta/1000000.0)
	fmt.Fprintln(f, "throughput = %0.2f\n", param.file_size*8.0*1000000/(delta*1024*1024))
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
	f := s.transfer.transcript
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
