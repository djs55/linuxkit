package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	logDump byte = iota
	logFollow
	logDumpFollow
)

// LogMessage is a single message sent to memlogd
type LogMessage struct {
	Time    time.Time // time message was received by memlogd
	Name    string    // name of the service that wrote the message
	Message string    // body of the message
}

func (m *LogMessage) String() string {
	return m.Time.Format(time.RFC3339) + " " + m.Name + " " + m.Message
}

// ParseLogMessage reconstructs a LogMessage from a line of text which
// looks like:
// <timestamp>,<origin>;<body>
func ParseLogMessage(line string) (*LogMessage, error) {
	bits := strings.SplitN(line, ";", 2)
	if len(bits) != 2 {
		return nil, errors.New("Failed to parse log message: " + line)
	}
	bits2 := strings.Split(bits[0], ",")
	if len(bits2) < 2 {
		// There could be more parameters in future
		return nil, errors.New("Failed to parse log message: " + line)
	}
	Time, err := time.Parse(time.RFC3339, bits2[0])
	if err != nil {
		return nil, err
	}
	return &LogMessage{
		Time:    Time,
		Name:    bits2[1],
		Message: bits[1],
	}, nil
}

// LogFile is where we write LogMessages to
type LogFile struct {
	File         *os.File // active file handle
	Name         string   // filename of log file
	Dir          string   // log file directory
	BytesWritten int      // total number of bytes written so far
}

// NewLogFile creates a new LogFile
func NewLogFile(dir, name string) (*LogFile, error) {
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		return nil, err
	}
	return &LogFile{
		File:         f,
		Name:         name,
		Dir:          dir,
		BytesWritten: 0,
	}, nil
}

// Write appends a message to the log file
func (l *LogFile) Write(m *LogMessage) error {
	s := m.String()
	_, err := io.WriteString(l.File, s)
	if err == nil {
		l.BytesWritten += len(s)
	}
	return err
}

// Rotate closes the current log file, rotates the files and creates an
// empty log file.
func (l *LogFile) Rotate(maxLogFiles int) error {
	if err := l.File.Close(); err != nil {
		return err
	}
	path := filepath.Join(l.Dir, l.Name)
	for i := maxLogFiles - 1; i >= 0; i-- {
		newerFile := fmt.Sprintf("%s.%d", path, i-1)
		// special case: if index is 0 we omit the suffix i.e. we expect
		// foo foo.1 foo.2 up to foo.<maxLogFiles-1>
		if i == 0 {
			newerFile = path
		}
		olderFile := fmt.Sprintf("%s.%d", path, i)
		// overwrite the olderFile with the newerFile
		err := os.Rename(newerFile, olderFile)
		if os.IsNotExist(err) {
			// the newerFile does not exist
			continue
		}
		if err != nil {
			return err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	l.File = f
	l.BytesWritten = 0
	return nil
}

func main() {
	socketPath := flag.String("socket", "/var/run/memlogdq.sock", "memlogd log query socket")
	logDir := flag.String("log-dir", "/var/log", "Directory containing log files")
	maxLogFiles := flag.Int("max-log-files", 10, "Maximum number of rotated log files before deletion")
	maxLogSize := flag.Int("max-log-size", 1024, "Maximum size of a log file before rotation")
	flag.Parse()

	addr := net.UnixAddr{
		Name: *socketPath,
		Net:  "unix",
	}
	conn, err := net.DialUnix("unix", nil, &addr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	n, err := conn.Write([]byte{logDumpFollow})
	if err != nil || n < 1 {
		log.Fatalf("Failed to write request to memlogd socket: %s", err)
	}

	// map of service name to active log file
	logs := make(map[string]*LogFile)

	r := bufio.NewReader(conn)
	for {
		line, err := r.ReadString('\n')
		if err == io.EOF {
			return
		}
		if err != nil {
			log.Fatalf("Failed to read from memlogd: %s", err)
		}
		msg, err := ParseLogMessage(line)
		if err != nil {
			log.Println(err)
			continue
		}
		if strings.HasPrefix(msg.Name, "logwrite") {
			// don't log our own output in a loop
			continue
		}

		var logF *LogFile
		var ok bool
		if logF, ok = logs[msg.Name]; !ok {
			logF, err = NewLogFile(*logDir, msg.Name)
			if err != nil {
				log.Fatalf("Failed to create log file %s: %s", msg.Name, err)
			}
			logs[msg.Name] = logF
		}
		if err = logF.Write(msg); err != nil {
			log.Fatalf("Failed to write to log file %s: %s", msg.Name, err)
		}
		if logF.BytesWritten > *maxLogSize {
			logF.Rotate(*maxLogFiles)
		}
	}
}
