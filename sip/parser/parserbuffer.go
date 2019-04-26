// Forked from github.com/StefanKopieczek/gossip by @StefanKopieczek
package parser

import (
	"bufio"
	"bytes"
	"io"

	"github.com/masterclock/gosip/log"
)

// parserBuffer is a specialized buffer for use in the parser.
// It is written to via the non-blocking Write.
// It exposes various blocking read methods, which wait until the requested
// data is available, and then return it.
type parserBuffer struct {
	io.Writer
	buffer bytes.Buffer

	// Wraps parserBuffer.pipeReader
	reader *bufio.Reader

	// Don't access this directly except when closing.
	pipeReader *io.PipeReader

	logger log.LocalLogger
}

// Create a new parserBuffer object (see struct comment for object details).
// Note that resources owned by the parserBuffer may not be able to be GCed
// until the Dispose() method is called.
func newParserBuffer() *parserBuffer {
	var pb parserBuffer
	pb.pipeReader, pb.Writer = io.Pipe()
	pb.reader = bufio.NewReader(pb.pipeReader)
	pb.logger = log.NewSafeLocalLogger()
	return &pb
}

func (pb *parserBuffer) Log() log.Logger {
	return pb.logger.Log()
}

func (pb *parserBuffer) SetLog(logger log.Logger) {
	pb.logger.SetLog(logger)
}

// Block until the buffer contains at least one CRLF-terminated line.
// Return the line, excluding the terminal CRLF, and delete it from the buffer.
// Returns an error if the parserbuffer has been stopped.
func (pb *parserBuffer) NextLine() (response string, err error) {
	var buffer bytes.Buffer
	var data string
	var b byte

	// There has to be a better way!
	for {
		data, err = pb.reader.ReadString('\r')
		if err != nil {
			return
		}

		buffer.WriteString(data)

		b, err = pb.reader.ReadByte()
		if err != nil {
			return
		}

		buffer.WriteByte(b)
		if b == '\n' {
			response = buffer.String()
			response = response[:len(response)-2]
			pb.Log().Debugf("ParserBuffer %p returns line '%s'", pb, response)
			return
		}
	}
}

// Block until the buffer contains at least n characters.
// Return precisely those n characters, then delete them from the buffer.
func (pb *parserBuffer) NextChunk(n int) (response string, err error) {
	var data = make([]byte, n)

	var read int
	for total := 0; total < n; {
		read, err = pb.reader.Read(data[total:])
		total += read
		if err != nil {
			return
		}
	}

	response = string(data)
	pb.Log().Debugf("ParserBuffer %p returns chunk '%s'", pb, response)
	return
}

// Stop the parser buffer.
func (pb *parserBuffer) Stop() {
	pb.Log().Debugf("stopping ParserBuffer %p", pb)
	pb.pipeReader.Close()
	pb.Log().Debugf("ParserBuffer %p stopped", pb)
}
