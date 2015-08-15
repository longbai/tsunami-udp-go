package feiliu

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

/* retransmission request */
type Retransmission struct {
	RequestType uint16 /* the retransmission request type           */
	Block       uint32 /* the block number to retransmit {at}       */
	ErrorRate   uint32 /* the current error rate (in % x 1000)      */
}

type Retransmissions []Retransmission

func (r Retransmissions) Bytes() []byte {
	buf := bytes.NewBuffer(nil)
	for i := 0; i < len(r); i++ {
		binary.Write(buf, binary.BigEndian, r[i].RequestType)
		binary.Write(buf, binary.BigEndian, r[i].Block)
		binary.Write(buf, binary.BigEndian, r[i].ErrorRate)
	}
	return buf.Bytes()
}

/*------------------------------------------------------------------------
 * u_int64_t get_udp_in_errors();
 *
 * Tries to return the current value of the UDP Input Error counter
 * that might be available in /proc/net/snmp
 *------------------------------------------------------------------------*/
func Get_udp_in_errors() int64 {
	in, err := os.OpenFile("/proc/net/snmp", os.O_RDONLY, 0600)
	if err != nil {
		return 0
	}
	defer in.Close()
	reader := bufio.NewReader(in)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return 0
		}
		if strings.HasPrefix(line, "Udp:") && !strings.Contains(line, "InErrors") {
			strs := strings.Split(line, " ")
			if len(strs) < 3 {
				return 0
			}
			errCount, err := strconv.ParseInt(strs[2], 10, 64)
			if err != nil {
				return 0
			}
			return errCount
		}
	}
	return 0
}

/*------------------------------------------------------------------------
 * u_int64_t get_usec_since(struct timeval *old_time);
 *
 * Returns the number of microseconds that have elapsed between the
 * given time and the time of this call.
 *------------------------------------------------------------------------*/
func Get_usec_since(t time.Time) int64 {
	return (time.Now().UnixNano() - t.UnixNano()) / 1000
}

/*------------------------------------------------------------------------
 * u_char *prepare_proof(u_char *buffer, size_t bytes,
 *                       const u_char *secret, u_char *digest);
 *
 * Prepares an MD5 hash as proof that we know the same shared secret as
 * another system.  The null-terminated secret stored in [secret] is
 * repeatedly XORed over the data stored in [buffer] (which is of
 * length [bytes]).  The MD5 hash of the resulting buffer is then
 * stored in [digest].  The pointer to the digest is returned.
 *------------------------------------------------------------------------*/
func PrepareProof(data, secret []byte) [16]byte {
	for i := 0; i < len(data); i++ {
		data[i] ^= secret[i%len(secret)]
	}

	return md5.Sum(data)
}

/*------------------------------------------------------------------------
 * int fread_line(FILE *f, char *buffer, size_t buffer_length);
 *
 * Reads a newline-terminated line from the given file descriptor and
 * returns it, sans the newline character.  No buffering is done.
 * Returns 0 on success and a negative value on error.
 *------------------------------------------------------------------------*/
func ReadLine(reader io.Reader, length int) (string, error) {
	data := make([]byte, length)
	b := make([]byte, 1)
	i := 0
	for ; i < length; i++ {
		_, err := reader.Read(b)
		if err != nil {
			return "", err
		}
		if b[0] == 0 || b[0] == '\n' {
			break
		}
		data[i] = b[0]
	}
	return string(data[:i]), nil
}

func ParseFraction(fraction string) (numerator, denominator int64) {
	nums := strings.Split(fraction, "/")
	if len(nums) != 2 {
		return
	}
	numerator, _ = strconv.ParseInt(nums[0], 10, 64)
	denominator, _ = strconv.ParseInt(nums[1], 10, 64)
	return
}
