package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"
)

func usage() {
	fmt.Println("read filename [blocksize]")
	os.Exit(1)
}

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		usage()
	}
	f, err := os.OpenFile(args[0], os.O_RDONLY, 0)
	if err != nil {
		fmt.Println(err)
		usage()
	}
	info, err := f.Stat()
	if err != nil {
		fmt.Println(err)
		usage()
	}
	block_size := 32768
	if len(args) > 2 {
		x, err := strconv.ParseInt(args[1], 10, 32)
		if err != nil {
			fmt.Println(err)
			usage()
		}
		block_size = int(x)
	}
	block := make([]byte, block_size)
	tStart := time.Now()

	for err == nil {
		_, err = f.Read(block)
	}
	tStop := time.Now()

	usec := tStop.Sub(tStart).Nanoseconds() / 1000
	bits := info.Size() * 8

	fmt.Printf("Start time = %d.%06d\n", tStart.Unix(), (tStart.UnixNano()/1000)%1000000)
	fmt.Printf("Stop time  = %d.%06d\n", tStop.Unix(), (tStop.UnixNano()/1000)%1000000)
	fmt.Printf("Interval   = %0.3f sec\n", float64(usec)/1000000.0)
	fmt.Printf("Read speed = %0.3f Mbps\n", float64(bits)*1.0/float64(usec))
}
